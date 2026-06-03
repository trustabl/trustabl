package rules

// SupportedSchemaVersion is the rule-schema version this engine build
// understands. A rule pack declares the schema version it targets in its
// manifest.yaml; rulesource rejects any pack whose schema_version exceeds
// this constant (an older engine cannot evaluate rules that need predicates
// or schema fields it does not have compiled in).
//
// Bump this whenever a new predicate or schema field is added to the rules
// engine (schema.go / predicates.go / evaluator.go), or when a rule begins to
// depend on a new engine capability an older binary lacks.
//
// v9: added the `agents_md` component kind (AGENTS.md discovery). The
// repo-hygiene rules (CSDK-203 / OAI-202 / ADK-201) now accept AGENTS.md as a
// vendor-neutral agent-guidance doc, so a pack using it must be rejected by a
// pre-v9 engine that does not discover AGENTS.md (which would otherwise
// over-fire those rules on AGENTS.md-only repos).
const SupportedSchemaVersion = 9
