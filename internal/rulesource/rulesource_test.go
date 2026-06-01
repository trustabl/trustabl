package rulesource

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

const goodPack = "schema_version: 1\n"

func cfgFor(remote, cache string) Config {
	return Config{RepoURL: remote, CacheDir: cache}
}

func TestResolve_FreshClone(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	wantSHA := newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})
	cache := filepath.Join(dir, "cache")

	res, err := Resolve(cfgFor(remote, cache), 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.SHA != wantSHA {
		t.Errorf("SHA = %q, want %q", res.SHA, wantSHA)
	}
	if res.FromCache {
		t.Error("FromCache = true on a fresh online clone")
	}
	if _, err := fs.ReadFile(res.FS, "manifest.yaml"); err != nil {
		t.Errorf("resolved FS missing manifest.yaml: %v", err)
	}
}

func TestResolve_CacheHitNoReclone(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})
	cache := filepath.Join(dir, "cache")

	first, err := Resolve(cfgFor(remote, cache), 1)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	second, err := Resolve(cfgFor(remote, cache), 1)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if first.SHA != second.SHA {
		t.Errorf("SHA drifted across resolves: %q vs %q", first.SHA, second.SHA)
	}
}

func TestResolve_NetworkFailFallsBackToCache(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})
	cache := filepath.Join(dir, "cache")

	primed, err := Resolve(cfgFor(remote, cache), 1)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	// Point at a dead remote: must fall back to the cached pack.
	res, err := Resolve(cfgFor(filepath.Join(dir, "gone"), cache), 1)
	if err != nil {
		t.Fatalf("fallback Resolve: %v", err)
	}
	if !res.FromCache {
		t.Error("FromCache = false after a network-failure fallback")
	}
	if res.SHA != primed.SHA {
		t.Errorf("fallback SHA = %q, want cached %q", res.SHA, primed.SHA)
	}
}

func TestResolve_NoCacheNoNetworkErrNoRules(t *testing.T) {
	dir := t.TempDir()
	_, err := Resolve(cfgFor(filepath.Join(dir, "gone"), filepath.Join(dir, "cache")), 1)
	if !errors.Is(err, ErrNoRules) {
		t.Errorf("err = %v, want ErrNoRules", err)
	}
}

func TestResolve_NoUpdateUsesCacheOnly(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})
	cache := filepath.Join(dir, "cache")

	noUpdate := cfgFor(remote, cache)
	noUpdate.NoUpdate = true
	if _, err := Resolve(noUpdate, 1); !errors.Is(err, ErrNoRules) {
		t.Errorf("NoUpdate on empty cache: err = %v, want ErrNoRules", err)
	}
	if _, err := Resolve(cfgFor(remote, cache), 1); err != nil {
		t.Fatalf("prime: %v", err)
	}
	res, err := Resolve(noUpdate, 1)
	if err != nil {
		t.Fatalf("NoUpdate after prime: %v", err)
	}
	if !res.FromCache {
		t.Error("FromCache = false for a NoUpdate resolve")
	}
}

func TestResolve_IncompatibleSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	newFixtureRepo(t, remote, map[string]string{"manifest.yaml": "schema_version: 99\n"})
	cache := filepath.Join(dir, "cache")

	_, err := Resolve(cfgFor(remote, cache), 1)
	if !errors.Is(err, ErrNoCompatibleRules) {
		t.Errorf("err = %v, want ErrNoCompatibleRules", err)
	}
}

func TestPull_FetchesAndRecordsCurrent(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	wantSHA := newFixtureRepo(t, remote, map[string]string{"manifest.yaml": goodPack})
	cache := filepath.Join(dir, "cache")

	res, err := Pull(cfgFor(remote, cache), 1)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if res.SHA != wantSHA {
		t.Errorf("SHA = %q, want %q", res.SHA, wantSHA)
	}
	if sha, ok := readCurrent(cache); !ok || sha != wantSHA {
		t.Errorf("current pointer = (%q,%v), want (%q,true)", sha, ok, wantSHA)
	}
}

func TestPull_NetworkFailErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Pull(cfgFor(filepath.Join(dir, "gone"), filepath.Join(dir, "cache")), 1); err == nil {
		t.Fatal("Pull against a dead remote returned nil error")
	}
}

func TestValidateRepoURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://github.com/trustabl/trustabl-rules", false},
		{"ssh://git@github.com/trustabl/trustabl-rules.git", false},
		{"git@github.com:trustabl/trustabl-rules.git", false},
		{"/local/path/to/rules", false}, // bare local path: legitimate offline source
		{`C:\local\rules`, false},       // Windows drive path: scheme "c" treated as local
		{"git://example.com/rules.git", true},
		{"file:///etc/passwd", true},
	}
	for _, c := range cases {
		if err := validateRepoURL(c.raw); (err != nil) != c.wantErr {
			t.Errorf("validateRepoURL(%q) err=%v, wantErr=%v", c.raw, err, c.wantErr)
		}
	}
}
