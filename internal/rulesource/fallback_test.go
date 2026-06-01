package rulesource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

// TestCloneInto_LocalFSFailureIsFatal asserts that a local filesystem failure
// during install (here: the cache "directory" is actually a regular file, so
// the first MkdirAll fails) surfaces as a fatalResolveError, so callers can
// distinguish it from a remote-contact failure and refuse to fall back.
func TestCloneInto_LocalFSFailureIsFatal(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})

	// cacheDir exists as a regular file → os.MkdirAll(cacheDir) must fail.
	cacheFile := filepath.Join(dir, "cache-is-a-file")
	if err := os.WriteFile(cacheFile, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed cache file: %v", err)
	}

	_, err := cloneInto(context.Background(), remote, "", cacheFile)
	if err == nil {
		t.Fatal("cloneInto into a file-path cache returned nil error")
	}
	var fe *fatalResolveError
	if !errors.As(err, &fe) {
		t.Errorf("err is not fatalResolveError: %v", err)
	}
}

// TestResolve_LocalInstallFailureNotMaskedAsCache is the regression test for
// the finding: when a valid cached pack exists AND the install of a freshly
// resolved pack fails locally, Resolve must propagate the failure rather than
// silently degrade to the stale cached pack. A masked failure here trains the
// operator to trust rules that never installed.
func TestResolve_LocalInstallFailureNotMaskedAsCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")

	// Prime the cache from remote A so a valid fallback exists.
	remoteA := filepath.Join(dir, "remoteA")
	newFixtureRepo(t, remoteA, map[string]string{"manifest.yaml": goodPack})
	if _, err := Resolve(cfgFor(remoteA, cache), 1); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	// Remote B has different content → a different, uncached SHA, so Resolve
	// will attempt a clone/install for it.
	remoteB := filepath.Join(dir, "remoteB")
	newFixtureRepo(t, remoteB, map[string]string{
		"manifest.yaml": goodPack,
		"extra.yaml":    "x: 1\n",
	})

	// Simulate a local install fault during that clone.
	orig := cloneIntoFn
	cloneIntoFn = func(_ context.Context, url string, refName plumbing.ReferenceName, cacheDir string) (string, error) {
		return "", &fatalResolveError{errors.New("simulated disk-full during install")}
	}
	defer func() { cloneIntoFn = orig }()

	_, err := Resolve(cfgFor(remoteB, cache), 1)
	if err == nil {
		t.Fatal("Resolve masked a local install failure as a cache hit; want a propagated error")
	}
	if errors.Is(err, ErrNoRules) {
		t.Errorf("got ErrNoRules, want the underlying install failure surfaced: %v", err)
	}
}

// TestResolve_RemoteCloneFailureStillFallsBack guards the offline story: a
// remote-contact failure during clone (NOT a fatalResolveError) must still
// degrade gracefully to the cached pack. This must not regress when local
// failures start propagating.
func TestResolve_RemoteCloneFailureStillFallsBack(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")

	remoteA := filepath.Join(dir, "remoteA")
	primedSHA := newFixtureRepo(t, remoteA, map[string]string{"manifest.yaml": goodPack})
	primed, err := Resolve(cfgFor(remoteA, cache), 1)
	if err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	if primed.SHA != primedSHA {
		t.Fatalf("primed SHA = %q, want %q", primed.SHA, primedSHA)
	}

	remoteB := filepath.Join(dir, "remoteB")
	newFixtureRepo(t, remoteB, map[string]string{
		"manifest.yaml": goodPack,
		"extra.yaml":    "x: 1\n",
	})

	orig := cloneIntoFn
	cloneIntoFn = func(_ context.Context, url string, refName plumbing.ReferenceName, cacheDir string) (string, error) {
		return "", errors.New("connection reset during clone") // plain, non-fatal
	}
	defer func() { cloneIntoFn = orig }()

	res, err := Resolve(cfgFor(remoteB, cache), 1)
	if err != nil {
		t.Fatalf("want graceful fallback, got error: %v", err)
	}
	if !res.FromCache {
		t.Error("FromCache = false; want cache fallback on a remote clone failure")
	}
	if res.SHA != primedSHA {
		t.Errorf("fallback SHA = %q, want cached %q", res.SHA, primedSHA)
	}
}
