package rules

// SupportedSchemaVersion is the rule-schema version this engine build
// understands. A rule pack declares the schema version it targets in its
// manifest.yaml; rulesource rejects any pack whose schema_version exceeds
// this constant (an older engine cannot evaluate rules that need predicates
// or schema fields it does not have compiled in).
//
// Bump this whenever a new predicate or schema field is added to the rules
// engine (schema.go / predicates.go / evaluator.go).
const SupportedSchemaVersion = 3
