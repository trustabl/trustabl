package rulesign_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

func mkPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// TestKeyring_RotationOverlap locks the documented rotation mechanism (overlapping
// validity windows): during the overlap BOTH the outgoing and incoming keys
// verify; after the outgoing key's not_after, only the incoming key verifies.
func TestKeyring_RotationOverlap(t *testing.T) {
	pubA, privA := mkPair(t)
	pubB, privB := mkPair(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ring := rulesign.NewKeyring(
		rulesign.Key{ID: "A", PublicKey: pubA, NotBefore: t0, NotAfter: t0.Add(48 * time.Hour)},
		rulesign.Key{ID: "B", PublicKey: pubB, NotBefore: t0.Add(24 * time.Hour), NotAfter: t0.Add(96 * time.Hour)},
	)
	msg := []byte("statement-payload")
	sigA := ed25519.Sign(privA, msg)
	sigB := ed25519.Sign(privB, msg)

	overlap := t0.Add(36 * time.Hour) // inside both windows
	if err := ring.Verify("A", msg, sigA, overlap); err != nil {
		t.Fatalf("outgoing key in overlap must verify: %v", err)
	}
	if err := ring.Verify("B", msg, sigB, overlap); err != nil {
		t.Fatalf("incoming key in overlap must verify: %v", err)
	}

	afterA := t0.Add(72 * time.Hour) // past A's window, inside B's
	if err := ring.Verify("A", msg, sigA, afterA); !errors.Is(err, rulesign.ErrKeyExpired) {
		t.Fatalf("outgoing key past not_after: want ErrKeyExpired, got %v", err)
	}
	if err := ring.Verify("B", msg, sigB, afterA); err != nil {
		t.Fatalf("incoming key must keep verifying after the cutover: %v", err)
	}
}

// TestKeyring_RevocationByRemoval locks the revocation mechanism: a key dropped
// from the keyring (the next engine build) no longer verifies anything.
func TestKeyring_RevocationByRemoval(t *testing.T) {
	pubA, privA := mkPair(t)
	msg := []byte("m")
	sig := ed25519.Sign(privA, msg)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if err := rulesign.NewKeyring(rulesign.Key{ID: "A", PublicKey: pubA}).Verify("A", msg, sig, at); err != nil {
		t.Fatalf("pre-revocation must verify: %v", err)
	}
	if err := rulesign.NewKeyring().Verify("A", msg, sig, at); !errors.Is(err, rulesign.ErrUnknownKeyID) {
		t.Fatalf("post-revocation (key removed): want ErrUnknownKeyID, got %v", err)
	}
}

// TestParseKeyring_Windows: a two-key keyring with overlapping windows parses
// (rotation is representable); an inverted window (not_after before not_before)
// is rejected.
func TestParseKeyring_Windows(t *testing.T) {
	pubA, _ := mkPair(t)
	pubB, _ := mkPair(t)
	b64 := func(p ed25519.PublicKey) string { return base64.StdEncoding.EncodeToString(p) }

	ok := fmt.Sprintf(`{"keys":[
      {"id":"A","public_key":%q,"not_before":"2026-01-01T00:00:00Z","not_after":"2026-03-01T00:00:00Z"},
      {"id":"B","public_key":%q,"not_before":"2026-02-01T00:00:00Z","not_after":"2026-04-01T00:00:00Z"}]}`, b64(pubA), b64(pubB))
	if r, err := rulesign.ParseKeyring([]byte(ok)); err != nil || r.Empty() {
		t.Fatalf("two-key overlapping keyring must parse: err=%v empty=%v", err, r.Empty())
	}

	inverted := fmt.Sprintf(`{"keys":[{"id":"X","public_key":%q,"not_before":"2026-03-01T00:00:00Z","not_after":"2026-01-01T00:00:00Z"}]}`, b64(pubA))
	if _, err := rulesign.ParseKeyring([]byte(inverted)); err == nil {
		t.Fatal("ParseKeyring accepted a key whose not_after precedes not_before")
	}
}

// TestVerifyStatement_FreshnessBoundary pins the inclusive expiry boundary:
// a statement is fresh AT exactly its expiry instant and stale one tick later.
// Locks the `at.After(Expires)` semantics against an off-by-one flip.
func TestVerifyStatement_FreshnessBoundary(t *testing.T) {
	pub, priv := mkPair(t)
	ring := rulesign.NewKeyring(rulesign.Key{ID: "k", PublicKey: pub})
	const expires = "2026-06-22T00:00:00Z"
	raw := signStatement(t, priv, "k", "production", 7, testDigest, "2026-06-08T00:00:00Z", expires)
	stmt, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatal(err)
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	p := rulesign.VerifyParams{Channel: "production", ExpectedDigest: testDigest}
	if err := rulesign.VerifyStatement(ring, stmt, p, exp); err != nil {
		t.Fatalf("at exactly Expires must be fresh: %v", err)
	}
	if err := rulesign.VerifyStatement(ring, stmt, p, exp.Add(time.Nanosecond)); !errors.Is(err, rulesign.ErrStatementExpired) {
		t.Fatalf("one tick past Expires must be ErrStatementExpired, got %v", err)
	}
}
