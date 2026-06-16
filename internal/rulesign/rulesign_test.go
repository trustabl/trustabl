package rulesign_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// fixedNow is an arbitrary in-window verification time; using a constant keeps
// the validity-window assertions independent of the wall clock.
var fixedNow = time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// TestKeyringVerify_Vectors is the ENG-2 acceptance matrix: a valid signature
// verifies, and every tampering or trust failure returns its specific typed
// error — the fail-closed contract the whole distribution model rests on.
func TestKeyringVerify_Vectors(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "trustabl-rules-2026-06"
	ring := rulesign.NewKeyring(rulesign.Key{
		ID:        keyID,
		PublicKey: pub,
		NotBefore: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	})
	msg := []byte("channel=production\nversion=42\ndigest=abc123")
	sig := ed25519.Sign(priv, msg)

	t.Run("valid signature in window", func(t *testing.T) {
		if err := ring.Verify(keyID, msg, sig, fixedNow); err != nil {
			t.Fatalf("valid signature must verify, got %v", err)
		}
	})

	t.Run("unknown key ID", func(t *testing.T) {
		if err := ring.Verify("no-such-key", msg, sig, fixedNow); !errors.Is(err, rulesign.ErrUnknownKeyID) {
			t.Fatalf("want ErrUnknownKeyID, got %v", err)
		}
	})

	t.Run("key not yet valid", func(t *testing.T) {
		before := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		if err := ring.Verify(keyID, msg, sig, before); !errors.Is(err, rulesign.ErrKeyNotYetValid) {
			t.Fatalf("want ErrKeyNotYetValid, got %v", err)
		}
	})

	t.Run("key expired", func(t *testing.T) {
		after := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := ring.Verify(keyID, msg, sig, after); !errors.Is(err, rulesign.ErrKeyExpired) {
			t.Fatalf("want ErrKeyExpired, got %v", err)
		}
	})

	t.Run("tampered message byte", func(t *testing.T) {
		bad := append([]byte(nil), msg...)
		bad[0] ^= 0x01
		if err := ring.Verify(keyID, bad, sig, fixedNow); !errors.Is(err, rulesign.ErrBadSignature) {
			t.Fatalf("want ErrBadSignature, got %v", err)
		}
	})

	t.Run("tampered signature byte", func(t *testing.T) {
		bad := append([]byte(nil), sig...)
		bad[0] ^= 0x01
		if err := ring.Verify(keyID, msg, bad, fixedNow); !errors.Is(err, rulesign.ErrBadSignature) {
			t.Fatalf("want ErrBadSignature, got %v", err)
		}
	})

	t.Run("signature from a different key", func(t *testing.T) {
		_, otherPriv := genKey(t)
		otherSig := ed25519.Sign(otherPriv, msg)
		if err := ring.Verify(keyID, msg, otherSig, fixedNow); !errors.Is(err, rulesign.ErrBadSignature) {
			t.Fatalf("want ErrBadSignature for foreign signer, got %v", err)
		}
	})
}

// TestKey_UnboundedWindow checks that a zero NotBefore/NotAfter means "no
// bound", so a key with no declared window verifies at any time.
func TestKey_UnboundedWindow(t *testing.T) {
	pub, priv := genKey(t)
	ring := rulesign.NewKeyring(rulesign.Key{ID: "k", PublicKey: pub})
	msg := []byte("x")
	sig := ed25519.Sign(priv, msg)
	for _, at := range []time.Time{
		time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		if err := ring.Verify("k", msg, sig, at); err != nil {
			t.Errorf("unbounded key must verify at %v, got %v", at, err)
		}
	}
}

func TestParseKeyring(t *testing.T) {
	pub, _ := genKey(t)
	b64 := base64.StdEncoding.EncodeToString(pub)

	t.Run("valid", func(t *testing.T) {
		j := fmt.Sprintf(`{"keys":[{"id":"k1","public_key":%q,"not_before":"2026-06-01T00:00:00Z","not_after":"2026-12-01T00:00:00Z"}]}`, b64)
		ring, err := rulesign.ParseKeyring([]byte(j))
		if err != nil {
			t.Fatalf("ParseKeyring: %v", err)
		}
		if ring.Empty() {
			t.Fatal("ring should not be empty")
		}
	})

	t.Run("wrong public key length", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("too short"))
		j := fmt.Sprintf(`{"keys":[{"id":"k1","public_key":%q}]}`, short)
		if _, err := rulesign.ParseKeyring([]byte(j)); err == nil {
			t.Fatal("want error for wrong-length public key")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		j := `{"keys":[{"id":"k1","public_key":"!!!not-base64!!!"}]}`
		if _, err := rulesign.ParseKeyring([]byte(j)); err == nil {
			t.Fatal("want error for invalid base64")
		}
	})

	t.Run("empty id", func(t *testing.T) {
		j := fmt.Sprintf(`{"keys":[{"id":"","public_key":%q}]}`, b64)
		if _, err := rulesign.ParseKeyring([]byte(j)); err == nil {
			t.Fatal("want error for empty id")
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		j := fmt.Sprintf(`{"keys":[{"id":"k1","public_key":%q},{"id":"k1","public_key":%q}]}`, b64, b64)
		if _, err := rulesign.ParseKeyring([]byte(j)); err == nil {
			t.Fatal("want error for duplicate id")
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if _, err := rulesign.ParseKeyring([]byte(`{not json`)); err == nil {
			t.Fatal("want error for malformed json")
		}
	})
}

// TestEmptyKeyring_FailsClosed asserts an empty keyring rejects every key id —
// the fail-closed property releaseSource relies on when a build embeds no trust
// keys. It is tested against an explicitly-constructed empty keyring, NOT the
// embedded one: a release build's embedded keyring must now be NON-empty (the
// signing key is published — see TestEmbeddedKeyring_IsPopulated in
// embedded_test.go), so asserting the embedded keyring is empty would be wrong.
func TestEmptyKeyring_FailsClosed(t *testing.T) {
	ring := rulesign.NewKeyring()
	if !ring.Empty() {
		t.Fatal("NewKeyring() with no keys must be Empty()")
	}
	if err := ring.Verify("anything", []byte("m"), []byte("s"), fixedNow); !errors.Is(err, rulesign.ErrUnknownKeyID) {
		t.Fatalf("empty keyring must reject every key id, got %v", err)
	}
}

// TestCanonicalDigest_Deterministic locks the reproducibility contract: the
// digest is a pure function of (paths × contents), independent of map/walk
// order, and changes when any path or byte changes.
func TestCanonicalDigest_Deterministic(t *testing.T) {
	build := func() fstest.MapFS {
		return fstest.MapFS{
			"rules/claude_sdk/a.yaml": {Data: []byte("rule a\n")},
			"rules/openai_sdk/b.yaml": {Data: []byte("rule b\n")},
			"manifest.yaml":           {Data: []byte("schema_version: 9\n")},
			"requirements.json":       {Data: []byte(`{"predicates":[]}`)},
		}
	}

	d1, err := rulesign.CanonicalDigest(build())
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	d2, err := rulesign.CanonicalDigest(build())
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("same content, different digest: %s vs %s", d1, d2)
	}
	if len(d1) != 64 {
		t.Errorf("digest should be 64 hex chars (sha256), got %d: %s", len(d1), d1)
	}

	t.Run("content change flips digest", func(t *testing.T) {
		m := build()
		m["rules/claude_sdk/a.yaml"] = &fstest.MapFile{Data: []byte("rule a EDITED\n")}
		d, _ := rulesign.CanonicalDigest(m)
		if d == d1 {
			t.Fatal("editing a file must change the digest")
		}
	})

	t.Run("added file flips digest", func(t *testing.T) {
		m := build()
		m["rules/claude_sdk/c.yaml"] = &fstest.MapFile{Data: []byte("rule c\n")}
		d, _ := rulesign.CanonicalDigest(m)
		if d == d1 {
			t.Fatal("adding a file must change the digest")
		}
	})

	t.Run("renamed path flips digest", func(t *testing.T) {
		m := build()
		m["rules/claude_sdk/renamed.yaml"] = m["rules/claude_sdk/a.yaml"]
		delete(m, "rules/claude_sdk/a.yaml")
		d, _ := rulesign.CanonicalDigest(m)
		if d == d1 {
			t.Fatal("renaming a file must change the digest")
		}
	})
}
