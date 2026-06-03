package rulesource

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPackDir_PerSHA(t *testing.T) {
	got := packDir("/cache", "abc123")
	want := filepath.Join("/cache", "abc123")
	if got != want {
		t.Errorf("packDir = %q, want %q", got, want)
	}
}

func TestPackExists(t *testing.T) {
	cache := t.TempDir()
	if packExists(cache, "abc123") {
		t.Error("packExists true for absent pack")
	}
	if err := os.MkdirAll(packDir(cache, "abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !packExists(cache, "abc123") {
		t.Error("packExists false for present pack")
	}
}

func TestPruneCache_KeepsOnlyCurrent(t *testing.T) {
	cache := t.TempDir()
	for _, sha := range []string{"aaa", "bbb", "ccc"} {
		if err := os.MkdirAll(packDir(cache, sha), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A stale temp-clone dir (interrupted clone) should also be cleared.
	if err := os.MkdirAll(filepath.Join(cache, ".tmp-clone-123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrent(cache, "bbb"); err != nil {
		t.Fatal(err)
	}
	// Backdate the non-kept entries past the grace window so they are eligible
	// for pruning (freshly created dirs are protected — see the grace-window
	// test below).
	old := time.Now().Add(-2 * pruneGraceWindow)
	for _, name := range []string{"aaa", "ccc", ".tmp-clone-123"} {
		if err := os.Chtimes(filepath.Join(cache, name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	pruneCache(cache, "bbb")

	if !packExists(cache, "bbb") {
		t.Error("kept SHA bbb was removed")
	}
	if packExists(cache, "aaa") || packExists(cache, "ccc") {
		t.Error("stale pack dirs not pruned")
	}
	if _, err := os.Stat(filepath.Join(cache, ".tmp-clone-123")); !os.IsNotExist(err) {
		t.Error("stale temp-clone dir not pruned")
	}
	if sha, ok := readCurrent(cache); !ok || sha != "bbb" {
		t.Errorf("current pointer damaged: got (%q, %v), want (bbb, true)", sha, ok)
	}
}

func TestPruneCache_GraceWindowProtectsRecentPacks(t *testing.T) {
	// Regression (TR-149): a non-kept pack with a recent mtime may belong to a
	// concurrent in-flight scan that reads its files lazily via os.DirFS after
	// Resolve returns. Pruning it would fail that scan (notably on Windows), so
	// the grace window must spare it; only genuinely stale packs are pruned.
	cache := t.TempDir()
	for _, sha := range []string{"fresh", "stale", "keep"} {
		if err := os.MkdirAll(packDir(cache, sha), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * pruneGraceWindow)
	if err := os.Chtimes(packDir(cache, "stale"), old, old); err != nil {
		t.Fatal(err)
	}

	pruneCache(cache, "keep")

	if !packExists(cache, "keep") {
		t.Error("kept pack was removed")
	}
	if !packExists(cache, "fresh") {
		t.Error("recent (concurrent-scan) pack was pruned; grace window not honored")
	}
	if packExists(cache, "stale") {
		t.Error("stale pack was not pruned")
	}
}

func TestCurrentPointer_RoundTrip(t *testing.T) {
	cache := t.TempDir()
	if _, ok := readCurrent(cache); ok {
		t.Error("readCurrent ok=true on empty cache")
	}
	if err := writeCurrent(cache, "deadbeef"); err != nil {
		t.Fatalf("writeCurrent: %v", err)
	}
	sha, ok := readCurrent(cache)
	if !ok || sha != "deadbeef" {
		t.Errorf("readCurrent = (%q, %v), want (deadbeef, true)", sha, ok)
	}
}
