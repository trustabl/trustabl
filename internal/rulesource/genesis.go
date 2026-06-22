package rulesource

// genesisFloors is the build-embedded minimum statement version trusted for each
// signed channel: the anti-rollback floor a machine that has NEVER recorded a
// statement starts from. Without it, a fresh machine accepts any validly-signed
// statement on first contact (trust-on-first-use), so an attacker able to MITM
// that first resolve could pin the machine to an old (but legitimately signed,
// in-window) statement that points at an outdated bundle with known rule gaps.
//
// A channel with no entry has floor 0 — the correct state until that channel
// publishes its first statement. When the first production statement ships, set
// genesisFloors["production"] to that statement's version (and bump it on a
// deliberate cadence) so first contact rejects anything older. This is a
// build-time constant on purpose: it is only as fresh as the engine binary, the
// same trade-off as the embedded trust keyring.
var genesisFloors = map[string]int64{
	// The EXACT `version` of the first production statement published to
	// channel-production (2026-06-16T19:56:20Z) — an epoch-seconds integer under
	// rulesctl's default scheme, NOT an ordinal, copied verbatim from the signed
	// statement. A fresh machine rejects any production statement older than this
	// on first contact; later publishes have a larger epoch-seconds version and
	// advance the per-machine recorded floor from here. Bump this on a deliberate
	// cadence as the channel ages. A wrong unit silently disarms (too low) or
	// over-arms (too high) anti-rollback.
	"production": 1781639780,
}

// genesisFloor returns the embedded minimum trusted version for channel, or 0
// when none is set.
func genesisFloor(channel string) int64 {
	return genesisFloors[channel]
}

// GenesisFloor is the exported view of genesisFloor, so a cutover guard test can
// assert that a signed default channel ships with a non-zero floor (see
// cmd/trustabl). 0 means no floor is set for that channel.
func GenesisFloor(channel string) int64 {
	return genesisFloor(channel)
}
