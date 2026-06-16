package rulesign_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// TestEmbeddedKeyring_IsPopulated is the RUL-2 build guard: once the signing
// public key is embedded, a build must never regress to an empty trust root.
// An empty embedded keyring makes releaseSource.prepare refuse every signed
// resolve with ErrNoTrustKeys, so a build that defaults to (or is asked for) a
// signed channel while the keyring is empty would exit 2 on every scan. This
// test fails loudly if internal/rulesign/keyring.json is ever reverted to
// {"keys": []}.
func TestEmbeddedKeyring_IsPopulated(t *testing.T) {
	ring, err := rulesign.Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	if ring.Empty() {
		t.Fatal("embedded keyring is empty — a release build must embed a rule-signing public key (RUL-2)")
	}
}
