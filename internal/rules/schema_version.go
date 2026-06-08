package rules

// SupportedSchemaVersion is the highest rule-schema version this engine build
// understands. A rule pack declares the schema version it targets in its
// manifest.yaml.
//
// Forward-compatible loading (see rules.LoadLenient): the engine NO LONGER
// rejects a pack just because its schema_version exceeds this constant. It
// loads the pack leniently — a rule that references a scope, applies_to value,
// language, or predicate this build lacks is skipped (surfaced as a stderr
// warning, the META-005 info finding, and ScanResult.RulesSkipped), and the scan
// proceeds with the rules it does understand. A pack is refused only when it has no usable manifest
// (ErrNoCompatibleRules) or when NO rule is evaluable at all
// (ErrAllRulesIncompatible). This decouples additive rule updates from binary
// upgrades: shipping a new predicate no longer locks every older binary out of
// the entire pack — older binaries skip the rules they can't evaluate and keep
// running the rest.
//
// Bumping this constant is therefore an informational signal — it advertises
// this build's support level, drives the "rules are newer than this build"
// warning, and is folded into ScanID so the ID stays honest about the effective
// ruleset — NOT a fleet-wide gate. Bump it when this build gains a new predicate
// or schema field.
//
// Make BREAKING changes by RENAMING the predicate (or field), never by silently
// redefining an existing one: an old binary then sees an unknown key and skips
// that rule (safe) instead of mis-evaluating a predicate whose meaning changed.
// That rename discipline is what keeps lenient loading safe without a hard
// version gate.
const SupportedSchemaVersion = 10
