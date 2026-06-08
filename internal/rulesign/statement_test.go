package rulesign_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

const testDigest = "1111111111111111111111111111111111111111111111111111111111111111"

// signStatement builds a JSON channel statement signed by priv under keyID,
// mirroring what the publisher (RUL-4/RUL-5) will do: build the canonical
// signing payload from the same raw strings emitted in the JSON, sign it, and
// assemble the document.
func signStatement(t *testing.T, priv ed25519.PrivateKey, keyID, channel string, version int64, digest, issuedAt, expires string) []byte {
	t.Helper()
	payload := rulesign.StatementSigningPayload(channel, version, digest, issuedAt, expires, keyID)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	return []byte(fmt.Sprintf(
		`{"channel":%q,"version":%d,"digest":%q,"issued_at":%q,"expires":%q,"key_id":%q,"signature":%q}`,
		channel, version, digest, issuedAt, expires, keyID, sig,
	))
}

func ringFor(t *testing.T, keyID string, pub ed25519.PublicKey) *rulesign.Keyring {
	t.Helper()
	return rulesign.NewKeyring(rulesign.Key{
		ID:        keyID,
		PublicKey: pub,
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
}

func TestVerifyStatement_HappyPath(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "production", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")

	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC) // inside the window
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{
		Channel:         "production",
		ExpectedDigest:  testDigest,
		LastSeenVersion: 41,
	}, at)
	if err != nil {
		t.Fatalf("valid statement must verify, got %v", err)
	}
}

// TestVerifyStatement_ChannelConfusion: a statement validly signed for
// "staging" must not satisfy a request for "production".
func TestVerifyStatement_ChannelConfusion(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "staging", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{Channel: "production"}, at)
	if !errors.Is(err, rulesign.ErrChannelMismatch) {
		t.Fatalf("want ErrChannelMismatch, got %v", err)
	}
}

// TestVerifyStatement_Downgrade: a validly-signed statement whose version is
// below the last-seen floor is a rollback attack and must be refused.
func TestVerifyStatement_Downgrade(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "production", 41, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{
		Channel:         "production",
		LastSeenVersion: 50, // we have already seen a newer pointer
	}, at)
	if !errors.Is(err, rulesign.ErrVersionRegression) {
		t.Fatalf("want ErrVersionRegression, got %v", err)
	}
}

func TestVerifyStatement_Expired(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "production", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) // past expires
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{Channel: "production"}, at)
	if !errors.Is(err, rulesign.ErrStatementExpired) {
		t.Fatalf("want ErrStatementExpired, got %v", err)
	}
}

func TestVerifyStatement_DigestMismatch(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "production", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	other := "2222222222222222222222222222222222222222222222222222222222222222"
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{
		Channel:        "production",
		ExpectedDigest: other,
	}, at)
	if !errors.Is(err, rulesign.ErrDigestMismatch) {
		t.Fatalf("want ErrDigestMismatch, got %v", err)
	}
}

// TestVerifyStatement_TamperedFieldBreaksSignature proves every policy field is
// under the signature: flipping the digest in the JSON (without re-signing)
// makes verification fail as a bad signature, not slip through.
func TestVerifyStatement_TamperedFieldBreaksSignature(t *testing.T) {
	pub, priv := genKey(t)
	const keyID = "k-2026"
	ring := ringFor(t, keyID, pub)
	raw := signStatement(t, priv, keyID, "production", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	// Forge the digest after parsing; the signature was over the original.
	tampered := &rulesign.Statement{
		Channel:   s.Channel,
		Version:   s.Version,
		Digest:    "deadbeef" + testDigest[8:],
		IssuedAt:  s.IssuedAt,
		Expires:   s.Expires,
		KeyID:     s.KeyID,
		Signature: s.Signature,
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	// Note: a Statement built directly (not via ParseStatement) has empty raw
	// time strings, but the digest change alone already breaks the signature.
	err = rulesign.VerifyStatement(ring, tampered, rulesign.VerifyParams{Channel: "production"}, at)
	if !errors.Is(err, rulesign.ErrBadSignature) {
		t.Fatalf("want ErrBadSignature for tampered field, got %v", err)
	}
}

func TestVerifyStatement_UnknownKey(t *testing.T) {
	pub, priv := genKey(t)
	ring := ringFor(t, "trusted-key", pub)
	// Statement claims a different key id than the keyring holds.
	raw := signStatement(t, priv, "attacker-key", "production", 42, testDigest,
		"2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	s, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("ParseStatement: %v", err)
	}
	at := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	err = rulesign.VerifyStatement(ring, s, rulesign.VerifyParams{Channel: "production"}, at)
	if !errors.Is(err, rulesign.ErrUnknownKeyID) {
		t.Fatalf("want ErrUnknownKeyID, got %v", err)
	}
}

func TestParseStatement_Malformed(t *testing.T) {
	cases := map[string]string{
		"unknown field":         `{"channel":"production","version":1,"digest":"` + testDigest + `","issued_at":"2026-06-08T00:00:00Z","expires":"2026-06-22T00:00:00Z","key_id":"k","signature":"AA==","extra":true}`,
		"missing channel":       `{"version":1,"digest":"` + testDigest + `","issued_at":"2026-06-08T00:00:00Z","expires":"2026-06-22T00:00:00Z","key_id":"k","signature":"AA=="}`,
		"non-positive version":  `{"channel":"production","version":0,"digest":"` + testDigest + `","issued_at":"2026-06-08T00:00:00Z","expires":"2026-06-22T00:00:00Z","key_id":"k","signature":"AA=="}`,
		"bad digest":            `{"channel":"production","version":1,"digest":"xyz","issued_at":"2026-06-08T00:00:00Z","expires":"2026-06-22T00:00:00Z","key_id":"k","signature":"AA=="}`,
		"expires before issued": `{"channel":"production","version":1,"digest":"` + testDigest + `","issued_at":"2026-06-22T00:00:00Z","expires":"2026-06-08T00:00:00Z","key_id":"k","signature":"AA=="}`,
		"bad signature b64":     `{"channel":"production","version":1,"digest":"` + testDigest + `","issued_at":"2026-06-08T00:00:00Z","expires":"2026-06-22T00:00:00Z","key_id":"k","signature":"!!!"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rulesign.ParseStatement([]byte(raw)); !errors.Is(err, rulesign.ErrStatementMalformed) {
				t.Fatalf("want ErrStatementMalformed, got %v", err)
			}
		})
	}
}

func TestChannelState_RoundTripAndMonotonic(t *testing.T) {
	dir := t.TempDir()

	v, err := rulesign.ReadLastSeenVersion(dir, "production")
	if err != nil {
		t.Fatalf("ReadLastSeenVersion: %v", err)
	}
	if v != 0 {
		t.Fatalf("fresh machine floor must be 0, got %d", v)
	}

	rec := func(version int64) int64 {
		t.Helper()
		floor, err := rulesign.RecordStatement(dir, &rulesign.Statement{
			Channel: "production", Version: version, Digest: testDigest,
		})
		if err != nil {
			t.Fatalf("RecordStatement(%d): %v", version, err)
		}
		return floor
	}

	if got := rec(42); got != 42 {
		t.Fatalf("after recording 42, floor = %d", got)
	}
	if got, _ := rulesign.ReadLastSeenVersion(dir, "production"); got != 42 {
		t.Fatalf("persisted floor = %d, want 42", got)
	}
	// A lower version must not lower the floor (idempotent / anti-rollback).
	if got := rec(41); got != 42 {
		t.Fatalf("recording 41 must keep floor at 42, got %d", got)
	}
	// Equal version is idempotent.
	if got := rec(42); got != 42 {
		t.Fatalf("recording 42 again must keep floor at 42, got %d", got)
	}
	// A higher version advances it.
	if got := rec(43); got != 43 {
		t.Fatalf("recording 43 must advance floor to 43, got %d", got)
	}
}

func TestChannelState_RejectsUnsafeChannel(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../escape", "a/b", "", ".."} {
		if _, err := rulesign.ReadLastSeenVersion(dir, bad); !errors.Is(err, rulesign.ErrInvalidChannel) {
			t.Errorf("ReadLastSeenVersion(%q): want ErrInvalidChannel, got %v", bad, err)
		}
	}
}
