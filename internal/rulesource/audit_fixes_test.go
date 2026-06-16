package rulesource

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// TestResolveFromStatement_InvalidManifestDoesNotAdvanceFloor locks the M3 fix:
// a digest-verified bundle whose manifest is unusable must NOT advance the
// anti-rollback floor or persist the channel pointer — otherwise every later
// offline resolve re-reads the poisoned pointer and re-fails, wedging the channel.
func TestResolveFromStatement_InvalidManifestDoesNotAdvanceFloor(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	// Digest-valid bundle, but its manifest declares a non-positive schema_version,
	// which the manifest gate rejects.
	bad := fstest.MapFS{
		"manifest.yaml":        {Data: []byte("schema_version: 0\n")},
		"claude_sdk/pack.yaml": {Data: []byte("policy:\n  id: cs\n  name: cs\n  category: claude_sdk\n  description: t\nrules: []\n")},
	}
	digest, _ := rulesign.CanonicalDigest(bad)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()
	rs := newRS(ring, &fakeTransport{statement: raw, bundle: bad}, inWindow)

	if _, err := rs.Resolve(prodCfg(cache), 9); !errors.Is(err, ErrNoCompatibleRules) {
		t.Fatalf("want ErrNoCompatibleRules for a bad-manifest bundle, got %v", err)
	}
	// The floor must NOT have advanced, and no usable pointer recorded.
	if v, _ := rulesign.ReadLastSeenVersion(bundleRoot(cache), "production"); v != 0 {
		t.Errorf("anti-rollback floor advanced to %d on a manifest-invalid bundle, want 0", v)
	}
}

// TestFromCache_BelowGenesisFloorRefuses locks the L2 fix: a floor bump shipped
// in the engine build must be honored even on the offline serve path, so a cached
// version below the floor is refused rather than served.
func TestFromCache_BelowGenesisFloorRefuses(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()

	// Prime the cache with a good online resolve at version 7 (floor unset == 0).
	if _, err := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow).Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("priming Resolve: %v", err)
	}

	// Ship a build whose genesis floor for production is 100 (> the cached v7).
	prev := genesisFloors["production"]
	genesisFloors["production"] = 100
	t.Cleanup(func() { genesisFloors["production"] = prev })

	// Offline (transport down) must now REFUSE the cached v7, not serve it.
	offline := newRS(ring, &fakeTransport{statementErr: errors.New("network down")}, inWindow)
	if _, err := offline.Resolve(prodCfg(cache), 9); !errors.Is(err, rulesign.ErrVersionRegression) {
		t.Fatalf("offline serve of a below-floor cached version: want ErrVersionRegression, got %v", err)
	}
}

// TestInstallBundle_IdempotentWhenAlreadyPresent locks the M2 fix's pre-check: a
// second install of the same digest (the common concurrency outcome) is a no-op
// success, never a rename-onto-existing-dir fatal.
func TestInstallBundle_IdempotentWhenAlreadyPresent(t *testing.T) {
	root := t.TempDir()
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)

	if err := installBundle(root, digest, bundle); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !bundleExists(root, digest) {
		t.Fatal("bundle not present after first install")
	}
	// Second install of the identical digest must succeed without error.
	if err := installBundle(root, digest, bundle); err != nil {
		t.Fatalf("second install (already present) must be a no-op success, got %v", err)
	}
	if !bundleExists(root, digest) {
		t.Fatal("bundle disappeared after idempotent re-install")
	}
}

// TestInstallBundle_SelfHealsMarkerlessDest locks the L1 fix: a partial
// (markerless) dir already sitting at the digest path must be removed and
// replaced, not cause a rename failure that wedges the channel forever.
func TestInstallBundle_SelfHealsMarkerlessDest(t *testing.T) {
	root := t.TempDir()
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)

	dest := bundleDir(root, digest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "stale.yaml"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bundleExists(root, digest) {
		t.Fatal("precondition: a markerless dir must not look complete")
	}

	if err := installBundle(root, digest, bundle); err != nil {
		t.Fatalf("installBundle must self-heal a markerless dest, got %v", err)
	}
	if !bundleExists(root, digest) {
		t.Fatal("bundle not complete after self-heal")
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.yaml")); !os.IsNotExist(err) {
		t.Error("the stale partial file survived the reinstall")
	}
}

// TestPruneBundles_RemovesSupersededKeepsActiveAndChannels locks the bundle-cache
// pruner: a superseded bundle is removed, the active one is kept, and a bundle a
// channel still points at is NOT pruned even when superseded by the keep digest.
func TestPruneBundles_RemovesSupersededKeepsActiveAndChannels(t *testing.T) {
	root := t.TempDir()
	a := mkBundle()
	b := mkBundle()
	b["extra.yaml"] = &fstest.MapFile{Data: []byte("x: 1\n")}
	da, _ := rulesign.CanonicalDigest(a)
	db, _ := rulesign.CanonicalDigest(b)
	if err := installBundle(root, da, a); err != nil {
		t.Fatal(err)
	}
	if err := installBundle(root, db, b); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour) // past the grace window
	if err := os.Chtimes(bundleDir(root, da), old, old); err != nil {
		t.Fatal(err)
	}

	pruneBundles(root, db) // keep B; no channel references A
	if bundleExists(root, da) {
		t.Error("superseded bundle A should have been pruned")
	}
	if !bundleExists(root, db) {
		t.Error("active bundle B must be kept")
	}

	// Reinstall A, backdate it, and point a channel at it: now it must survive.
	if err := installBundle(root, da, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bundleDir(root, da), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := rulesign.RecordStatement(root, &rulesign.Statement{Channel: "staging", Version: 1, Digest: da}); err != nil {
		t.Fatal(err)
	}
	pruneBundles(root, db)
	if !bundleExists(root, da) {
		t.Error("a bundle a channel still points at must not be pruned")
	}
}

// TestRecordStatement_EqualVersionRefreshesPointer locks the L15 fix: a re-signed
// statement at the SAME version but a different digest updates the offline
// pointer (so it does not go stale), while the anti-rollback floor stays put.
func TestRecordStatement_EqualVersionRefreshesPointer(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	a := mkBundle()
	b := mkBundle()
	b["extra.yaml"] = &fstest.MapFile{Data: []byte("y: 2\n")}
	da, _ := rulesign.CanonicalDigest(a)
	db, _ := rulesign.CanonicalDigest(b)
	cache := t.TempDir()

	raw1 := signStatement(t, priv, "k", "production", 7, da, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	if _, err := newRS(ring, &fakeTransport{statement: raw1, bundle: a}, inWindow).Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// Re-sign at the SAME version 7, pointing at a different bundle.
	raw2 := signStatement(t, priv, "k", "production", 7, db, "2026-06-08T00:00:00Z", "2026-06-25T00:00:00Z")
	if _, err := newRS(ring, &fakeTransport{statement: raw2, bundle: b}, inWindow).Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("re-sign resolve: %v", err)
	}
	digest, version, _, found, err := rulesign.ChannelPointer(bundleRoot(cache), "production")
	if err != nil || !found {
		t.Fatalf("pointer: found=%v err=%v", found, err)
	}
	if digest != db {
		t.Errorf("offline pointer digest = %s, want the re-signed %s (stale)", digest, db)
	}
	if version != 7 {
		t.Errorf("anti-rollback floor = %d, want 7 (unchanged)", version)
	}
}

// TestFromCache_CorruptExpiryIsStale locks the L3 fix: an unparseable persisted
// expiry is treated as stale (fail-closed on the freshness signal), never
// silently served as fresh.
func TestFromCache_CorruptExpiryIsStale(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()
	if _, err := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow).Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Corrupt the persisted expiry to a non-RFC3339 value.
	statePath := filepath.Join(bundleRoot(cache), "channels", "production.json")
	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	st["expires"] = "not-a-timestamp"
	nb, _ := json.Marshal(st)
	if err := os.WriteFile(statePath, nb, 0o644); err != nil {
		t.Fatal(err)
	}

	// Offline resolve serves the cache but must flag Stale.
	res, err := newRS(ring, &fakeTransport{statementErr: errors.New("offline")}, inWindow).Resolve(prodCfg(cache), 9)
	if err != nil {
		t.Fatalf("offline resolve: %v", err)
	}
	if !res.FromCache || !res.Stale {
		t.Errorf("FromCache=%v Stale=%v, want true/true for a corrupt cached expiry", res.FromCache, res.Stale)
	}
}

// TestReleaseSource_Pull locks the explicit-fetch contract of the SIGNED path:
// Pull always contacts the remote and NEVER degrades to cache (unlike Resolve),
// and verification failures propagate.
func TestReleaseSource_Pull(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")

	t.Run("happy path resolves fresh, not from cache", func(t *testing.T) {
		res, err := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow).Pull(prodCfg(t.TempDir()), 9)
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if res.FromCache || res.SHA != digest {
			t.Errorf("FromCache=%v SHA=%q, want false/%s", res.FromCache, res.SHA, digest)
		}
	})

	t.Run("fetch error does NOT fall back to cache", func(t *testing.T) {
		cache := t.TempDir()
		if _, err := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow).Resolve(prodCfg(cache), 9); err != nil {
			t.Fatalf("prime: %v", err)
		}
		// A primed cache exists, but Pull must still error rather than serve it.
		if _, err := newRS(ring, &fakeTransport{statementErr: errors.New("network down")}, inWindow).Pull(prodCfg(cache), 9); err == nil {
			t.Fatal("Pull fell back to cache on a fetch error; it must fail loudly")
		}
	})

	t.Run("verification failure propagates", func(t *testing.T) {
		afterExpiry := func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
		if _, err := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, afterExpiry).Pull(prodCfg(t.TempDir()), 9); !errors.Is(err, rulesign.ErrStatementExpired) {
			t.Fatalf("want ErrStatementExpired, got %v", err)
		}
	})
}
