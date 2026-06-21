package rulesource

import (
	"errors"
	"os"
	"testing"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// realGenesisFloors snapshots the build-embedded genesis floors before the suite
// zeroes the runtime map (see TestMain). The production-floor pin test asserts
// against this snapshot; every other test then drives the floor explicitly from a
// clean (no-floor) baseline, so synthetic small-version statements aren't blocked
// by the real, epoch-seconds-scale production floor.
var realGenesisFloors = map[string]int64{}

func TestMain(m *testing.M) {
	for k, v := range genesisFloors {
		realGenesisFloors[k] = v
	}
	genesisFloors = map[string]int64{}
	os.Exit(m.Run())
}

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

// TestGenesisFloor_ProductionPinned freezes the production floor to the EXACT
// version of the first production statement (channel-production, published
// 2026-06-16T19:56:20Z). It guards against the entry being dropped or its unit
// changed — either silently disarms anti-rollback for every fresh machine that
// resolves production. Bump this in lockstep with genesis.go when the floor is
// deliberately advanced.
func TestGenesisFloor_ProductionPinned(t *testing.T) {
	const firstProductionVersion int64 = 1781639780
	if got := realGenesisFloors["production"]; got != firstProductionVersion {
		t.Fatalf("embedded genesisFloors[production] = %d, want %d (first published statement version)", got, firstProductionVersion)
	}
}
