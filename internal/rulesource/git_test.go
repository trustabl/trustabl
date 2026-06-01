package rulesource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newFixtureRepo creates a non-bare git repo at dir with one commit holding
// the given files, and returns the commit SHA. Used as a local "remote".
func newFixtureRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	h, err := wt.Commit("fixture", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return h.String()
}

func TestResolveRef_DefaultHEAD(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	want := newFixtureRepo(t, remote, map[string]string{"manifest.yaml": "schema_version: 1\n"})
	sha, _, err := resolveRef(context.Background(), remote, "")
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	if sha != want {
		t.Errorf("sha = %q, want %q", sha, want)
	}
}

func TestCloneInto_CopiesContent(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	want := newFixtureRepo(t, remote, map[string]string{
		"manifest.yaml":     "schema_version: 1\n",
		"claude_sdk/a.yaml": "policy: {}\n",
	})
	cache := filepath.Join(dir, "cache")
	_, name, err := resolveRef(context.Background(), remote, "")
	if err != nil {
		t.Fatalf("resolveRef: %v", err)
	}
	sha, err := cloneInto(context.Background(), remote, name, cache)
	if err != nil {
		t.Fatalf("cloneInto: %v", err)
	}
	// The returned SHA is the actual cloned HEAD, and the pack lands under a
	// directory named by that SHA.
	if sha != want {
		t.Errorf("cloneInto sha = %q, want %q", sha, want)
	}
	dest := packDir(cache, sha)
	if _, err := os.Stat(filepath.Join(dest, "manifest.yaml")); err != nil {
		t.Errorf("manifest.yaml not cloned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "claude_sdk", "a.yaml")); err != nil {
		t.Errorf("claude_sdk/a.yaml not cloned: %v", err)
	}
	// No temp clone directory is left behind after a successful install.
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-clone-") {
			t.Errorf("leftover temp clone dir: %s", e.Name())
		}
	}
}

// TestResolve_IgnoresPartialPack guards the cache-atomicity contract: a
// half-written pack directory (simulating an interrupted clone) must NOT be
// reused as if it were a complete clone. Because packExists keys on the
// resolved HEAD SHA and a real clone always lands via atomic rename, a stray
// partial directory under a *different* name is simply ignored, and the scan
// still resolves a valid pack.
func TestResolve_IgnoresPartialPack(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	want := newFixtureRepo(t, remote, map[string]string{"manifest.yaml": "schema_version: 1\n"})
	cache := filepath.Join(dir, "cache")

	// Simulate a partial clone left behind under some stale SHA name: a dir
	// that exists but holds no manifest.
	partial := packDir(cache, "0000000000000000000000000000000000000000")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatalf("seed partial: %v", err)
	}

	res, err := Resolve(Config{RepoURL: remote, CacheDir: cache}, 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.SHA != want {
		t.Errorf("resolved SHA = %q, want %q (the real HEAD, not the partial)", res.SHA, want)
	}
	if res.FromCache {
		t.Error("FromCache = true; expected a fresh clone, not a fallback")
	}
}

func TestResolveRef_NetworkError(t *testing.T) {
	if _, _, err := resolveRef(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), ""); err == nil {
		t.Fatal("expected error for nonexistent remote, got nil")
	}
}
