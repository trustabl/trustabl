package rulesign

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Verification failure modes. Each is a distinct, fail-closed outcome so the
// resolver (ENG-4) and the CLI can give an accurate, non-leaky message.
var (
	// ErrUnknownKeyID means the signature names a key ID absent from the trust
	// keyring — a forged or rotated-out signer. Wrapped with the offending ID.
	ErrUnknownKeyID = errors.New("rulesign: unknown signing key ID")
	// ErrKeyNotYetValid means the named key exists but its validity window has
	// not begun at the verification time.
	ErrKeyNotYetValid = errors.New("rulesign: signing key not yet valid")
	// ErrKeyExpired means the named key's validity window has ended — a revoked
	// or retired key. Anti-rollback: an old signature from a now-expired key
	// stops verifying once the window closes.
	ErrKeyExpired = errors.New("rulesign: signing key expired")
	// ErrBadSignature means the key is known and in-window but the signature
	// does not authenticate the message — tampered bytes or a wrong signer.
	ErrBadSignature = errors.New("rulesign: signature verification failed")
)

// Key is one entry in the trust keyring: a published Ed25519 public key with a
// validity window. Rotation is expressed as overlapping windows (introduce
// N+1, dual-sign during the overlap, let N's window close); revocation is
// removal from the published keyring in the next engine build.
type Key struct {
	ID        string
	PublicKey ed25519.PublicKey
	NotBefore time.Time // zero => no lower bound
	NotAfter  time.Time // zero => no upper bound
}

func (k Key) validAt(at time.Time) error {
	if !k.NotBefore.IsZero() && at.Before(k.NotBefore) {
		return ErrKeyNotYetValid
	}
	if !k.NotAfter.IsZero() && at.After(k.NotAfter) {
		return ErrKeyExpired
	}
	return nil
}

// Keyring is an immutable set of trusted signing keys indexed by ID. The
// engine embeds one at build time (see Embedded); callers must treat it as the
// whole of the trust root — nothing outside it is trusted.
type Keyring struct {
	keys map[string]Key
}

// NewKeyring builds a keyring from the given keys. A later key with a duplicate
// ID overwrites an earlier one; use ParseKeyring for the published JSON form,
// which rejects duplicates instead.
func NewKeyring(keys ...Key) *Keyring {
	m := make(map[string]Key, len(keys))
	for _, k := range keys {
		m[k.ID] = k
	}
	return &Keyring{keys: m}
}

// Empty reports whether the keyring trusts no keys. A build with an empty
// embedded keyring cannot verify anything, so the signed-rules path must refuse
// up front with a clear "no trust root" message rather than letting every
// signature fail as ErrUnknownKeyID.
func (r *Keyring) Empty() bool { return len(r.keys) == 0 }

// Verify checks that sig authenticates message under the key named by keyID,
// with that key in its validity window at time at. It fails closed: an unknown
// key, an out-of-window key, or a bad signature each return a typed error and
// never nil. at is the caller's notion of "now" (the resolver passes wall-clock
// time; tests pass a fixed instant).
func (r *Keyring) Verify(keyID string, message, sig []byte, at time.Time) error {
	k, ok := r.keys[keyID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKeyID, keyID)
	}
	if err := k.validAt(at); err != nil {
		return err
	}
	if len(k.PublicKey) != ed25519.PublicKeySize || !ed25519.Verify(k.PublicKey, message, sig) {
		return ErrBadSignature
	}
	return nil
}

// --- Published keyring JSON ---------------------------------------------------

type jsonKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`           // base64 std, 32-byte Ed25519
	NotBefore string `json:"not_before,omitempty"` // RFC3339
	NotAfter  string `json:"not_after,omitempty"`  // RFC3339
}

type jsonKeyring struct {
	Keys []jsonKey `json:"keys"`
}

// ParseKeyring decodes the published trust-keyring JSON (the artifact RUL-2
// generates and an engine build embeds). It validates every key — non-empty
// ID, well-formed 32-byte base64 public key, parseable optional RFC3339
// windows — and rejects duplicate IDs, so a malformed trust root fails the
// build rather than silently trusting fewer or wrong keys.
func ParseKeyring(data []byte) (*Keyring, error) {
	var jk jsonKeyring
	if err := json.Unmarshal(data, &jk); err != nil {
		return nil, fmt.Errorf("rulesign: parse keyring: %w", err)
	}
	keys := make([]Key, 0, len(jk.Keys))
	seen := make(map[string]bool, len(jk.Keys))
	for i, e := range jk.Keys {
		if e.ID == "" {
			return nil, fmt.Errorf("rulesign: keyring key %d has empty id", i)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("rulesign: duplicate keyring key id %q", e.ID)
		}
		seen[e.ID] = true

		pub, err := base64.StdEncoding.DecodeString(e.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("rulesign: key %q public_key: %w", e.ID, err)
		}
		if len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("rulesign: key %q public_key is %d bytes, want %d", e.ID, len(pub), ed25519.PublicKeySize)
		}
		k := Key{ID: e.ID, PublicKey: ed25519.PublicKey(pub)}
		if e.NotBefore != "" {
			t, err := time.Parse(time.RFC3339, e.NotBefore)
			if err != nil {
				return nil, fmt.Errorf("rulesign: key %q not_before: %w", e.ID, err)
			}
			k.NotBefore = t
		}
		if e.NotAfter != "" {
			t, err := time.Parse(time.RFC3339, e.NotAfter)
			if err != nil {
				return nil, fmt.Errorf("rulesign: key %q not_after: %w", e.ID, err)
			}
			k.NotAfter = t
		}
		// An always-invalid key (window ends before it begins) is a publishing
		// mistake; reject it loudly rather than embed a key that can never verify.
		if !k.NotBefore.IsZero() && !k.NotAfter.IsZero() && k.NotAfter.Before(k.NotBefore) {
			return nil, fmt.Errorf("rulesign: key %q not_after precedes not_before", e.ID)
		}
		keys = append(keys, k)
	}
	return NewKeyring(keys...), nil
}

//go:embed keyring.json
var embeddedKeyringJSON []byte

// Embedded returns the trust keyring compiled into this build from keyring.json.
// It is the live trust root for the signed-rules path: only statements signed by
// a key in here verify. A build must ship a NON-empty keyring (enforced by
// TestEmbeddedKeyring_IsPopulated) — an empty one makes releaseSource refuse
// every signed resolve with ErrNoTrustKeys. Rotation/revocation is expressed by
// editing keyring.json (overlapping validity windows to rotate; removal to
// revoke) and shipping a new build.
func Embedded() (*Keyring, error) {
	return ParseKeyring(embeddedKeyringJSON)
}
