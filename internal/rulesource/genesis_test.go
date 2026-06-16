package rulesource

import (
	"errors"
	"testing"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// TestReleaseSource_GenesisFloorRejectsBelowOnFreshMachine proves the
// build-embedded genesis floor blunts trust-on-first-use: a brand-new machine
// (no recorded last-seen version) must still reject a validly-signed statement
// whose version is below the floor this build was shipped with — so an attacker
// who MITMs a first resolve cannot pin the machine to an old, validly-signed
// statement pointing at a stale bundle.
func TestReleaseSource_GenesisFloorRejectsBelowOnFreshMachine(t *testing.T) {
	prev := genesisFloors["production"]
	genesisFloors["production"] = 100
	t.Cleanup(func() { genesisFloors["production"] = prev })

	pub, priv := mkKey(t)
	ring := mkRing(t, "k", pub)
	bundle := mkBundle()
	digest, _ := rulesign.CanonicalDigest(bundle)

	// Fresh cache: ReadLastSeenVersion would return 0, so without the genesis
	// floor this validly-signed v50 statement would be accepted.
	below := signStatement(t, priv, "k", "production", 50, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	rs := newRS(ring, &fakeTransport{statement: below, bundle: bundle}, inWindow)
	if _, err := rs.Resolve(prodCfg(t.TempDir()), 9); !errors.Is(err, rulesign.ErrVersionRegression) {
		t.Fatalf("below-genesis statement on a fresh machine: want ErrVersionRegression, got %v", err)
	}

	// At/above the floor resolves cleanly on an equally-fresh machine.
	ok := signStatement(t, priv, "k", "production", 150, digest, "2026-06-08T00:00:00Z", "2026-06-22T00:00:00Z")
	rs2 := newRS(ring, &fakeTransport{statement: ok, bundle: bundle}, inWindow)
	if _, err := rs2.Resolve(prodCfg(t.TempDir()), 9); err != nil {
		t.Fatalf("at-or-above-genesis statement on a fresh machine: %v", err)
	}
}

// TestGenesisFloor_DefaultsToZero documents that an unset channel has no floor —
// the correct state until a channel publishes its first statement, after which
// its floor is set to that version.
func TestGenesisFloor_DefaultsToZero(t *testing.T) {
	if got := genesisFloor("a-channel-with-no-floor"); got != 0 {
		t.Fatalf("genesisFloor(unset) = %d, want 0", got)
	}
}
