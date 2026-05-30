package ingestion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// countingProgress is a CloneProgress that records the calls fetchTreeToDir
// drives, so tests can assert the receiving-objects bar was actually fed.
type countingProgress struct {
	total    int
	advances int
	details  []string
	resets   int
}

func (c *countingProgress) SetTotal(n int)     { c.total = n }
func (c *countingProgress) Advance(string)     { c.advances++ }
func (c *countingProgress) SetDetail(d string) { c.details = append(c.details, d) }
func (c *countingProgress) ResetPhase()        { c.resets++ }

// cloneObserver translates packfile parse callbacks into clone progress. Tested
// directly (no transport) because it's the custom glue most likely to regress,
// and because a nil prog must never panic.
func TestCloneObserver(t *testing.T) {
	p := &countingProgress{}
	o := &cloneObserver{prog: p}
	if err := o.OnHeader(5); err != nil {
		t.Fatalf("OnHeader: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := o.OnInflatedObjectHeader(plumbing.AnyObject, 0, 0); err != nil {
			t.Fatalf("OnInflatedObjectHeader: %v", err)
		}
	}
	if p.total != 5 {
		t.Errorf("total = %d, want 5", p.total)
	}
	if p.advances != 3 {
		t.Errorf("advances = %d, want 3", p.advances)
	}

	// A nil prog (caller passed no progress) must be a no-op, not a panic.
	nilObs := &cloneObserver{prog: nil}
	if err := nilObs.OnHeader(2); err != nil {
		t.Fatalf("nil-prog OnHeader: %v", err)
	}
	if err := nilObs.OnInflatedObjectHeader(plumbing.AnyObject, 0, 0); err != nil {
		t.Fatalf("nil-prog OnInflatedObjectHeader: %v", err)
	}
}

// fetchTreeToDir end-to-end over the local git transport: build a real repo,
// fetch its HEAD tree into a fresh dir, and assert both nested working-tree
// files land and the progress bar was fed a total + advances. The local
// transport shells out to git-upload-pack, so skip cleanly where that binary
// isn't installed rather than fail on an environment gap.
func TestFetchTreeToDirLocal(t *testing.T) {
	if _, err := exec.LookPath("git-upload-pack"); err != nil {
		t.Skip("git-upload-pack not on PATH; skipping local-transport clone test")
	}

	srcDir := t.TempDir()
	repo, err := git.PlainInit(srcDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "hello.py"), []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "pkg", "mod.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for _, f := range []string{"hello.py", "pkg/mod.py"} {
		if _, err := wt.Add(f); err != nil {
			t.Fatalf("Add %s: %v", f, err)
		}
	}
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	dest := t.TempDir()
	prog := &countingProgress{}
	if err := fetchTreeToDir(context.Background(), srcDir, dest, prog); err != nil {
		t.Fatalf("fetchTreeToDir: %v", err)
	}

	for _, rel := range []string{"hello.py", filepath.FromSlash("pkg/mod.py")} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected %s in dest tree: %v", rel, err)
		}
	}
	if prog.total == 0 {
		t.Error("progress total never set (OnHeader not wired)")
	}
	if prog.advances == 0 {
		t.Error("progress never advanced (object headers not wired)")
	}
}

// safeJoin must keep extracted tree paths inside the destination root — a
// malicious or malformed tree entry ("../x", absolute, or one that climbs out)
// must be rejected, never written outside the clone dir.
func TestSafeJoin(t *testing.T) {
	root := filepath.FromSlash("/tmp/clone")
	ok := []struct{ rel, want string }{
		{"a.py", filepath.Join(root, "a.py")},
		{"src/agents/loop.py", filepath.Join(root, "src/agents/loop.py")},
		{"./nested/x", filepath.Join(root, "nested/x")},
	}
	for _, c := range ok {
		got, err := safeJoin(root, c.rel)
		if err != nil {
			t.Errorf("safeJoin(%q) unexpected error: %v", c.rel, err)
			continue
		}
		if got != c.want {
			t.Errorf("safeJoin(%q) = %q, want %q", c.rel, got, c.want)
		}
	}
	// Paths that climb out via .. must be rejected on every platform.
	escaping := []string{"../evil", "a/../../evil", "../../x"}
	for _, rel := range escaping {
		if _, err := safeJoin(root, rel); err == nil {
			t.Errorf("safeJoin(%q) should have rejected an escaping path", rel)
		}
	}
	// Rooted/absolute-style inputs are platform-dependent (POSIX absolute,
	// Windows drive-less). The invariant is only that the result never escapes
	// root: either an error, or a path contained within root.
	for _, rel := range []string{"/etc/passwd", `\windows\system32`} {
		got, err := safeJoin(root, rel)
		if err != nil {
			continue // rejected — fine
		}
		if r, _ := filepath.Rel(root, got); r == ".." || filepath.IsAbs(r) ||
			len(r) >= 2 && r[0] == '.' && r[1] == '.' {
			t.Errorf("safeJoin(%q) = %q escaped root %q", rel, got, root)
		}
	}
}
