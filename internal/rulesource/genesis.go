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
	// "production": <first published version>,  // set at the first production publish
}

// genesisFloor returns the embedded minimum trusted version for channel, or 0
// when none is set.
func genesisFloor(channel string) int64 {
	return genesisFloors[channel]
}
