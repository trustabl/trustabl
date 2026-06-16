// Package rulepub is the publisher-side counterpart to internal/rulesign: it
// generates signing keypairs, packs a rule-pack directory into the exact bundle
// the engine will re-derive a digest from, and signs channel statements the
// engine will verify. It is imported ONLY by cmd/rulesctl (the CI/maintainer
// tool), never by the scanner binary — so no private-key or signing code links
// into the shipped `trustabl` binary, preserving rulesign's verify-only contract.
package rulepub

import (
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// GenerateKeypair returns a fresh Ed25519 signing keypair as (seed, public key).
// The 32-byte seed is the only secret a publisher needs — store it in the CI
// signing secret; ed25519.NewKeyFromSeed reconstructs the full private key from
// it. The 32-byte public key is non-secret and goes into the engine's embedded
// trust keyring (internal/rulesign/keyring.json).
func GenerateKeypair() (seed []byte, pub ed25519.PublicKey, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("rulepub: generate ed25519 key: %w", err)
	}
	// priv is seed||public (64 bytes); the seed is the first 32 and is what we
	// persist, so signing is reproducible from the secret alone.
	return priv.Seed(), pub, nil
}

// StatementParams is the fully-resolved input to one signed channel statement.
// All time fields are RFC3339 strings: the exact bytes signed are reused
// verbatim in the emitted JSON (see SignStatement), so the engine rebuilds the
// signing payload with no reserialization drift.
type StatementParams struct {
	Channel  string
	Version  int64
	Digest   string // 64-char lowercase hex — a bundle's rulesign.CanonicalDigest
	KeyID    string
	IssuedAt string // RFC3339
	Expires  string // RFC3339
}

// SignStatement validates p, signs the canonical statement payload with the
// Ed25519 key derived from seed, and returns the statement JSON document. The
// payload is built with rulesign.StatementSigningPayload — the same encoding the
// engine verifies against — and the issued_at/expires strings emitted in the
// JSON are byte-identical to those signed.
func SignStatement(seed []byte, p StatementParams) ([]byte, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("rulepub: signing seed is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	if err := validateChannelName(p.Channel); err != nil {
		return nil, err
	}
	if p.KeyID == "" {
		return nil, fmt.Errorf("rulepub: empty key id")
	}
	if p.Version <= 0 {
		return nil, fmt.Errorf("rulepub: version must be positive, got %d", p.Version)
	}
	if !isHex64(p.Digest) {
		return nil, fmt.Errorf("rulepub: digest must be 64-char lowercase hex, got %q", p.Digest)
	}
	issued, err := time.Parse(time.RFC3339, p.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("rulepub: issued_at: %w", err)
	}
	expires, err := time.Parse(time.RFC3339, p.Expires)
	if err != nil {
		return nil, fmt.Errorf("rulepub: expires: %w", err)
	}
	if expires.Before(issued) {
		return nil, fmt.Errorf("rulepub: expires %s precedes issued_at %s", p.Expires, p.IssuedAt)
	}

	priv := ed25519.NewKeyFromSeed(seed)
	payload := rulesign.StatementSigningPayload(p.Channel, p.Version, p.Digest, p.IssuedAt, p.Expires, p.KeyID)
	sig := ed25519.Sign(priv, payload)

	doc := wireStatement{
		Channel:   p.Channel,
		Version:   p.Version,
		Digest:    p.Digest,
		IssuedAt:  p.IssuedAt,
		Expires:   p.Expires,
		KeyID:     p.KeyID,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rulepub: marshal statement: %w", err)
	}
	return append(out, '\n'), nil
}

// validateChannelName mirrors the engine's channel-name rule (rulesign
// channelstate.validChannelName) and additionally reserves "git", which the CLI
// uses as the unsigned-source sentinel — a channel literally named "git" would be
// signable but unselectable, so refuse it at publish time.
func validateChannelName(name string) error {
	if name == "" {
		return fmt.Errorf("rulepub: empty channel")
	}
	if name == "git" {
		return fmt.Errorf("rulepub: %q is a reserved CLI token, not a valid channel name", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("rulepub: invalid channel name %q", name)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return fmt.Errorf("rulepub: invalid channel name %q (allowed: lowercase letters, digits, '.', '_', '-')", name)
		}
	}
	return nil
}

// VerifyBundle is the producer-side self-verify gate: it confirms a candidate
// statement against a trust keyring and the local bundle directory the statement
// commits to. It checks signature, channel binding, freshness, and the bundle
// digest — i.e. all of the engine's resolve-time checks EXCEPT the anti-rollback /
// genesis-version floor, which is per-channel state the engine enforces at scan
// time and the producer does not have here. CI runs this on the freshly signed
// candidate BEFORE promoting a channel, so a statement that fails any of these
// dimensions can never be promoted; a version-regression is still caught by the
// fleet at resolve time. now is the verification time.
func VerifyBundle(statementJSON []byte, bundleDir string, ring *rulesign.Keyring, channel string, now time.Time) error {
	stmt, err := rulesign.ParseStatement(statementJSON)
	if err != nil {
		return err
	}
	digest, err := rulesign.CanonicalDigest(os.DirFS(bundleDir))
	if err != nil {
		return fmt.Errorf("rulepub: digest bundle dir: %w", err)
	}
	return rulesign.VerifyStatement(ring, stmt, rulesign.VerifyParams{
		Channel:        channel,
		ExpectedDigest: digest,
	}, now)
}

// wireStatement mirrors the engine's statement JSON shape (field names are the
// contract; rulesign.ParseStatement decodes exactly these keys).
type wireStatement struct {
	Channel   string `json:"channel"`
	Version   int64  `json:"version"`
	Digest    string `json:"digest"`
	IssuedAt  string `json:"issued_at"`
	Expires   string `json:"expires"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// Bundle writes a gzipped canonical tar of the rule pack rooted at dir to w and
// returns its rulesign.CanonicalDigest. Producer and engine share one
// serialization (rulesign.WriteCanonicalTar), so the digest returned here equals
// the one the engine recomputes after downloading and unpacking the bundle.
//
// It refuses a tree whose manifest.yaml is missing or declares a non-positive
// schema_version: such a pack would pass the engine's signature and digest
// checks and only THEN fail ErrNoCompatibleRules (exit 2) on the whole fleet.
// Catching it at publish time is the whole point.
func Bundle(dir string, w io.Writer) (digest string, err error) {
	fsys := os.DirFS(dir)
	if err := validateManifest(fsys); err != nil {
		return "", err
	}
	digest, err = rulesign.CanonicalDigest(fsys)
	if err != nil {
		return "", fmt.Errorf("rulepub: digest bundle: %w", err)
	}
	gz := gzip.NewWriter(w)
	if err := rulesign.WriteCanonicalTar(gz, fsys); err != nil {
		return "", fmt.Errorf("rulepub: write bundle: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("rulepub: close gzip: %w", err)
	}
	return digest, nil
}

// validateManifest enforces the same gate the engine applies (manifest.yaml
// present at the FS root with a positive schema_version) before a bundle ships.
func validateManifest(fsys fs.FS) error {
	b, err := fs.ReadFile(fsys, "manifest.yaml")
	if err != nil {
		return fmt.Errorf("rulepub: bundle has no manifest.yaml at its root: %w", err)
	}
	var m struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("rulepub: unparseable manifest.yaml: %w", err)
	}
	if m.SchemaVersion <= 0 {
		return fmt.Errorf("rulepub: manifest.yaml schema_version must be positive, got %d", m.SchemaVersion)
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
