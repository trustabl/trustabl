package rulesign

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// statementDomain is the domain-separation tag prefixed to every signed
// channel-statement payload. The version suffix makes a future encoding change
// unambiguous: an old verifier sees a payload it cannot reconstruct and the
// signature simply fails closed rather than being misinterpreted.
const statementDomain = "trustabl-rules-channel-statement-v1"

// Channel-statement failure modes, all fail-closed. Signature failures surface
// as the keyring errors (ErrUnknownKeyID / ErrKeyExpired / ErrBadSignature).
var (
	// ErrStatementMalformed means the statement JSON is unparseable, has an
	// unknown field, is missing a required field, or carries an out-of-range
	// value (non-positive version, expires-before-issued, non-hex digest).
	ErrStatementMalformed = errors.New("rulesign: malformed channel statement")
	// ErrChannelMismatch means a validly-signed statement names a different
	// channel than the engine requested — a channel-confusion attempt.
	ErrChannelMismatch = errors.New("rulesign: channel statement is for a different channel")
	// ErrStatementExpired means the statement's freshness window has passed; the
	// fleet must treat the channel as stale rather than trust an old pointer.
	ErrStatementExpired = errors.New("rulesign: channel statement expired")
	// ErrVersionRegression means the statement's version is below the
	// last-seen version for its channel — a rollback attack pinning the fleet
	// to an older, possibly-vulnerable bundle.
	ErrVersionRegression = errors.New("rulesign: channel statement version regressed")
	// ErrDigestMismatch means the downloaded bundle's recomputed digest does
	// not equal the digest the (authenticated) statement commits to.
	ErrDigestMismatch = errors.New("rulesign: bundle digest does not match statement")
)

// Statement is a signed channel pointer: it binds a channel to one bundle
// (by digest) at a monotonic version, within a freshness window. It is the
// single signed object the engine resolves at scan time — verifying it
// establishes which bundle to trust for a channel.
type Statement struct {
	Channel   string
	Version   int64
	Digest    string // hex sha256 — a bundle's CanonicalDigest
	IssuedAt  time.Time
	Expires   time.Time
	KeyID     string
	Signature []byte

	// Raw RFC3339 strings exactly as received, used to rebuild the signed
	// payload without reformatting drift (sub-second precision, offset form).
	issuedAtRaw string
	expiresRaw  string
}

// StatementSigningPayload returns the exact bytes a channel statement's
// signature must cover: a domain-separated, fixed-order, line-based encoding.
// The publisher (RUL-4 / RUL-5) MUST construct the signed message with this
// identical encoding — reusing the same issued_at/expires strings it emits in
// the JSON — so a statement signed in CI verifies here with no reserialization
// drift. key_id is signed too, binding the statement to its signer.
func StatementSigningPayload(channel string, version int64, digest, issuedAt, expires, keyID string) []byte {
	return []byte(strings.Join([]string{
		statementDomain,
		"channel=" + channel,
		"version=" + strconv.FormatInt(version, 10),
		"digest=" + digest,
		"issued_at=" + issuedAt,
		"expires=" + expires,
		"key_id=" + keyID,
	}, "\n"))
}

// SigningPayload returns the signed bytes for this parsed statement.
func (s *Statement) SigningPayload() []byte {
	return StatementSigningPayload(s.Channel, s.Version, s.Digest, s.issuedAtRaw, s.expiresRaw, s.KeyID)
}

type wireStatement struct {
	Channel   string `json:"channel"`
	Version   int64  `json:"version"`
	Digest    string `json:"digest"`
	IssuedAt  string `json:"issued_at"`
	Expires   string `json:"expires"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// ParseStatement decodes and structurally validates a channel statement.
// It does NOT verify the signature or apply channel/freshness/version policy —
// that is VerifyStatement's job. Strict decoding (unknown fields rejected) plus
// required-field and range checks turn a corrupt or truncated statement into a
// loud ErrStatementMalformed rather than a half-populated struct.
func ParseStatement(data []byte) (*Statement, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w wireStatement
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStatementMalformed, err)
	}
	if w.Channel == "" || w.Digest == "" || w.IssuedAt == "" || w.Expires == "" || w.KeyID == "" || w.Signature == "" {
		return nil, fmt.Errorf("%w: missing required field", ErrStatementMalformed)
	}
	if w.Version <= 0 {
		return nil, fmt.Errorf("%w: version must be positive, got %d", ErrStatementMalformed, w.Version)
	}
	if !isHex64(w.Digest) {
		return nil, fmt.Errorf("%w: digest must be 64-char lowercase hex", ErrStatementMalformed)
	}
	issuedAt, err := time.Parse(time.RFC3339, w.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: issued_at: %v", ErrStatementMalformed, err)
	}
	expires, err := time.Parse(time.RFC3339, w.Expires)
	if err != nil {
		return nil, fmt.Errorf("%w: expires: %v", ErrStatementMalformed, err)
	}
	if expires.Before(issuedAt) {
		return nil, fmt.Errorf("%w: expires precedes issued_at", ErrStatementMalformed)
	}
	sig, err := base64.StdEncoding.DecodeString(w.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: signature: %v", ErrStatementMalformed, err)
	}
	return &Statement{
		Channel:     w.Channel,
		Version:     w.Version,
		Digest:      w.Digest,
		IssuedAt:    issuedAt,
		Expires:     expires,
		KeyID:       w.KeyID,
		Signature:   sig,
		issuedAtRaw: w.IssuedAt,
		expiresRaw:  w.Expires,
	}, nil
}

// VerifyParams is the policy a statement is checked against.
type VerifyParams struct {
	// Channel is the channel the engine asked for; the statement must name it.
	Channel string
	// ExpectedDigest, when non-empty, is the recomputed digest of the bundle
	// the engine actually downloaded; the statement must commit to it. Empty
	// skips the binding (e.g. when verifying a statement before fetching).
	ExpectedDigest string
	// LastSeenVersion is the anti-rollback floor — the highest version this
	// channel has previously presented. The statement's version must be >= it.
	LastSeenVersion int64
}

// VerifyStatement enforces the full trust check, in order: signature first
// (nothing is trusted until authenticated), then channel binding, freshness,
// anti-rollback, and the optional bundle-digest binding. Any failure returns a
// typed error and the statement must not be used. at is the caller's "now".
func VerifyStatement(ring *Keyring, s *Statement, p VerifyParams, at time.Time) error {
	// 1. Authenticity. Verify the signature before reading any field as fact;
	// the payload covers every policy-relevant field, so a flipped channel,
	// version, digest, or expiry invalidates the signature here.
	if err := ring.Verify(s.KeyID, s.SigningPayload(), s.Signature, at); err != nil {
		return err
	}
	// 2. Channel binding — reject a valid statement aimed at another channel.
	if s.Channel != p.Channel {
		return fmt.Errorf("%w: statement is for %q, requested %q", ErrChannelMismatch, s.Channel, p.Channel)
	}
	// 3. Freshness — reject an expired pointer.
	if at.After(s.Expires) {
		return fmt.Errorf("%w: expired %s", ErrStatementExpired, s.expiresRaw)
	}
	// 4. Anti-rollback — reject a regression below the last-seen version.
	if s.Version < p.LastSeenVersion {
		return fmt.Errorf("%w: version %d < last-seen %d", ErrVersionRegression, s.Version, p.LastSeenVersion)
	}
	// 5. Digest binding — reject a bundle whose content does not match.
	if p.ExpectedDigest != "" && s.Digest != p.ExpectedDigest {
		return fmt.Errorf("%w: statement %s, bundle %s", ErrDigestMismatch, s.Digest, p.ExpectedDigest)
	}
	return nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
