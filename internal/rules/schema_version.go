package rules

// SupportedSchemaVersion is the rule-schema version this engine build
// understands. A rule pack declares the schema version it targets in its
// manifest.yaml; rulesource rejects any pack whose schema_version exceeds
// this constant (an older engine cannot evaluate rules that need predicates
// or schema fields it does not have compiled in).
//
// Bump this ONLY when the rule grammar changes in a way an older binary would
// mis-parse or mis-evaluate: a new predicate, a new schema field, or changed
// evaluator semantics (schema.go / predicates.go / evaluator.go). A rule that
// merely behaves better on a newer engine but degrades to a benign result on
// an older one (e.g. a discovery capability an old binary lacks) does NOT
// warrant a bump — gating the whole pack out of every old binary over one
// rule's edge-case behavior is disproportionate. Reserve this for true
// grammar breaks; let capability drift degrade gracefully instead.
const SupportedSchemaVersion = 8
