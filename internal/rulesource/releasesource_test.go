package rulesource

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// inWindow is a verification time inside every test statement's freshness
// window and inside the test keyring's validity window.
func inWindow() time.Time { return time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC) }

func mkKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func mkRing(t *testing.T, keyID string, pub ed25519.PublicKey) *rulesign.Keyring {
	t.Helper()
	return rulesign.NewKeyring(rulesign.Key{
		ID:        keyID,
		PublicKey: pub,
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
}

// mkBundle is a minimal valid rule bundle: a manifest the schema gate accepts
// plus one (empty) pack file, enough to exercise install + digest binding.
func mkBundle() fstest.MapFS {
	return fstest.MapFS{
		"manifest.yaml":        {Data: []byte("schema_version: 9\n")},
		"claude_sdk/pack.yaml": {Data: []byte("policy:\n  id: cs\n  name: cs\n  category: claude_sdk\n  description: t\nrules: []\n")},
	}
}

func signStatement(t *testing.T, priv ed25519.PrivateKey, keyID, channel string, version int64, digest, issuedAt, expires string) []byte {
	t.Helper()
	payload := rulesign.StatementSigningPayload(channel, version, digest, issuedAt, expires, keyID)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	return []byte(fmt.Sprintf(
		`{"channel":%q,"version":%d,"digest":%q,"issued_at":%q,"expires":%q,"key_id":%q,"signature":%q}`,
		channel, version, digest, issuedAt, expires, keyID, sig,
	))
}

type fakeTransport struct {
	statement    []byte
	bundle       fs.FS
	statementErr error
	bundleErr    error
}

func (f *fakeTransport) FetchStatement(repoURL, channel string) ([]byte, error) {
	if f.statementErr != nil {
		return nil, f.statementErr
	}
	return f.statement, nil
}

func (f *fakeTransport) FetchBundle(repoURL, digest string) (fs.FS, error) {
	if f.bundleErr != nil {
		return nil, f.bundleErr
	}
	return f.bundle, nil
}

func newRS(ring *rulesign.Keyring, tr ChannelTransport, now func() time.Time) *releaseSource {
	return &releaseSource{keyring: ring, transport: tr, now: now}
}

func prodCfg(cache string) Config {
	return Config{Channel: "production", RepoURL: "https://example.com/rules", CacheDir: cache}
}

// TestReleaseSource_HappyPath is the ENG-4 end-to-end: a signed statement +
// matching bundle resolve, install to the content-addressed cache, expose a
// scannable FS, and advance the channel's anti-rollback floor.
func TestReleaseSource_HappyPath(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, err := rulesign.CanonicalDigest(bundle)
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()
	rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)

	res, err := rs.Resolve(prodCfg(cache), 9)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.SHA != digest {
		t.Errorf("SHA = %q, want digest %q", res.SHA, digest)
	}
	if res.SchemaVersion != 9 || res.SchemaNewer {
		t.Errorf("schema = %d newer=%v, want 9/false", res.SchemaVersion, res.SchemaNewer)
	}
	if res.FromCache || res.Stale {
		t.Errorf("fresh online resolve: FromCache=%v Stale=%v, want false/false", res.FromCache, res.Stale)
	}
	if _, err := fs.ReadFile(res.FS, "manifest.yaml"); err != nil {
		t.Errorf("installed bundle FS missing manifest.yaml: %v", err)
	}
	if v, _ := rulesign.ReadLastSeenVersion(cache, "production"); v != 7 {
		t.Errorf("anti-rollback floor = %d, want 7", v)
	}
}

func TestReleaseSource_RefusesTampering(t *testing.T) {
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)

	t.Run("bad signature (foreign signer)", func(t *testing.T) {
		pubA, _ := mkKey(t)
		_, privB := mkKey(t)
		ring := mkRing(t, "k", pubA)
		raw := signStatement(t, privB, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, rulesign.ErrBadSignature) {
			t.Fatalf("want ErrBadSignature, got %v", err)
		}
	})

	t.Run("unknown key id", func(t *testing.T) {
		pub, priv := mkKey(t)
		ring := mkRing(t, "trusted", pub)
		raw := signStatement(t, priv, "attacker", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, rulesign.ErrUnknownKeyID) {
			t.Fatalf("want ErrUnknownKeyID, got %v", err)
		}
	})

	t.Run("channel confusion", func(t *testing.T) {
		pub, priv := mkKey(t)
		ring := mkRing(t, "k", pub)
		raw := signStatement(t, priv, "k", "staging", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9) // asks for production
		if !errors.Is(err, rulesign.ErrChannelMismatch) {
			t.Fatalf("want ErrChannelMismatch, got %v", err)
		}
	})

	t.Run("expired statement", func(t *testing.T) {
		pub, priv := mkKey(t)
		ring := mkRing(t, "k", pub)
		raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		afterExpiry := func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, afterExpiry)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, rulesign.ErrStatementExpired) {
			t.Fatalf("want ErrStatementExpired, got %v", err)
		}
	})

	t.Run("version rollback", func(t *testing.T) {
		pub, priv := mkKey(t)
		ring := mkRing(t, "k", pub)
		cache := t.TempDir()
		// Pre-seed a higher floor (we have already seen version 50).
		if _, err := rulesign.RecordStatement(cache, &rulesign.Statement{Channel: "production", Version: 50, Digest: digest}); err != nil {
			t.Fatalf("seed floor: %v", err)
		}
		raw := signStatement(t, priv, "k", "production", 40, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
		_, err := rs.Resolve(prodCfg(cache), 9)
		if !errors.Is(err, rulesign.ErrVersionRegression) {
			t.Fatalf("want ErrVersionRegression, got %v", err)
		}
	})

	t.Run("bundle digest mismatch", func(t *testing.T) {
		pub, priv := mkKey(t)
		ring := mkRing(t, "k", pub)
		// Statement commits to the good digest, but the transport serves a
		// different bundle.
		tampered := mkBundle()
		tampered["sneaky.yaml"] = &fstest.MapFile{Data: []byte("evil: true\n")}
		raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
		rs := newRS(ring, &fakeTransport{statement: raw, bundle: tampered}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, rulesign.ErrDigestMismatch) {
			t.Fatalf("want ErrDigestMismatch, got %v", err)
		}
	})

	t.Run("malformed statement", func(t *testing.T) {
		pub, _ := mkKey(t)
		ring := mkRing(t, "k", pub)
		rs := newRS(ring, &fakeTransport{statement: []byte(`{not json`), bundle: bundle}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, rulesign.ErrStatementMalformed) {
			t.Fatalf("want ErrStatementMalformed, got %v", err)
		}
	})
}

// TestReleaseSource_NoTrustKeysRefuses: a build with an empty embedded keyring
// (RUL-2 not yet landed) refuses up front rather than failing every signature.
func TestReleaseSource_NoTrustKeysRefuses(t *testing.T) {
	rs := newRS(rulesign.NewKeyring(), &fakeTransport{}, inWindow)
	_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
	if !errors.Is(err, ErrNoTrustKeys) {
		t.Fatalf("want ErrNoTrustKeys (no trust root), got %v", err)
	}
}

func TestReleaseSource_OfflineFallback(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()

	// Prime the cache with one good online resolve.
	online := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
	if _, err := online.Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("priming Resolve: %v", err)
	}

	t.Run("NoUpdate serves cache without touching transport", func(t *testing.T) {
		// A transport that errors on any call proves it is not consulted.
		rs := newRS(ring, &fakeTransport{statementErr: errors.New("must not be called")}, inWindow)
		cfg := prodCfg(cache)
		cfg.NoUpdate = true
		res, err := rs.Resolve(cfg, 9)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.FromCache || res.SHA != digest {
			t.Errorf("FromCache=%v SHA=%q, want true/%q", res.FromCache, res.SHA, digest)
		}
	})

	t.Run("remote unreachable degrades to cache", func(t *testing.T) {
		rs := newRS(ring, &fakeTransport{statementErr: errors.New("network down")}, inWindow)
		res, err := rs.Resolve(prodCfg(cache), 9)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.FromCache {
			t.Errorf("want FromCache true on offline fallback")
		}
	})

	t.Run("expired cache is served but flagged Stale", func(t *testing.T) {
		afterExpiry := func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
		rs := newRS(ring, &fakeTransport{statementErr: errors.New("network down")}, afterExpiry)
		res, err := rs.Resolve(prodCfg(cache), 9)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.FromCache || !res.Stale {
			t.Errorf("FromCache=%v Stale=%v, want true/true", res.FromCache, res.Stale)
		}
	})

	t.Run("offline with empty cache refuses", func(t *testing.T) {
		rs := newRS(ring, &fakeTransport{statementErr: errors.New("network down")}, inWindow)
		_, err := rs.Resolve(prodCfg(t.TempDir()), 9)
		if !errors.Is(err, ErrNoRules) {
			t.Fatalf("want ErrNoRules with no cache, got %v", err)
		}
	})
}

// TestReleaseSource_SecondResolveSkipsRefetch verifies the content-addressed
// cache: once a digest is installed, a re-resolve must not re-fetch the bundle.
func TestReleaseSource_SecondResolveSkipsRefetch(t *testing.T) {
	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)
	raw := signStatement(t, priv, "k", "production", 7, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	cache := t.TempDir()

	first := newRS(ring, &fakeTransport{statement: raw, bundle: bundle}, inWindow)
	if _, err := first.Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	// Second resolve: same statement, but a transport that errors if the bundle
	// is fetched. The digest is already cached, so FetchBundle must not run.
	second := newRS(ring, &fakeTransport{statement: raw, bundleErr: errors.New("must not refetch")}, inWindow)
	if _, err := second.Resolve(prodCfg(cache), 9); err != nil {
		t.Fatalf("second Resolve should reuse cached bundle, got %v", err)
	}
}

func TestSourceFor_SelectsByChannel(t *testing.T) {
	if _, ok := SourceFor(Config{}).(gitSource); !ok {
		t.Error("empty Channel must select gitSource")
	}
	if _, ok := SourceFor(Config{Channel: "production"}).(*releaseSource); !ok {
		t.Error("non-empty Channel must select releaseSource")
	}
}
