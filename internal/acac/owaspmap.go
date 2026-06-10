package acac

// OWASP mapping (spec §7): a static, versioned table in the engine, pinned to
// the 2026-06 snapshot of two taxonomies:
//
//   - ASI01–ASI10 — OWASP Top 10 for Agentic Applications (2026):
//     ASI01 Agent Goal Hijack · ASI02 Tool Misuse · ASI03 Identity &
//     Privilege Abuse · ASI04 Agentic Supply Chain · ASI05 Unexpected Code
//     Execution · ASI06 Memory & Context Poisoning · ASI07 Insecure
//     Inter-Agent Communication · ASI08 Cascading Agent Failures · ASI09
//     Human-Agent Trust Exploitation · ASI10 Rogue Agents.
//   - AST01–AST10 — OWASP Agentic Skills Top 10 (incubator; IDs may churn
//     upstream — ours change only with a spec bump). Used here with the
//     semantics the rulebook already cites: AST03 over-privileged skills,
//     AST04 insecure metadata, AST05 prompt injection via skill content.
//
// This table is deliberately NOT a RuleDef field (rule grounding lives in the
// rulebook, per the engine CLAUDE.md) and NOT a rules-repo YAML key (under
// strict KnownFields decoding that would be a schema-version bump). An
// unmapped rule simply has no entry: the owasp key is omitted, never invented
// and never empty-listed.
//
// Seed scope: the high/critical rules of the claude_sdk, openai_sdk, and
// claude_skill packs, plus the spec's two anchor examples (CSDK-014,
// CSDK-110). Reliability-only rules (missing timeouts, disabled error
// handlers) are deliberately unmapped — the ASI taxonomy is a security
// taxonomy and stretching it to pure-reliability findings would dilute both.
// Completing the map across packs is a separate editorial workstream.
var owaspMap = map[string][]string{
	// claude_sdk — agent scope
	"CSDK-101": {"ASI03"},          // subagent granted Bash
	"CSDK-103": {"ASI03"},          // permissionMode bypassPermissions
	"CSDK-104": {"ASI03"},          // subagent granted fs-write built-ins
	"CSDK-105": {"ASI03"},          // subagent granted WebFetch
	"CSDK-120": {"ASI03"},          // TS bypassPermissions
	"CSDK-130": {"ASI03"},          // TS main agent granted Bash
	"CSDK-131": {"ASI03"},          // TS main agent fs-write/web-fetch built-ins
	// claude_sdk — tool scope
	"CSDK-004": {"ASI02"},          // path param used in I/O without validation
	"CSDK-009": {"ASI02"},          // SSRF: caller-controlled URL
	"CSDK-010": {"ASI02", "ASI05"}, // TS tool shells out
	"CSDK-011": {"ASI05"},          // TS tool evaluates dynamic code
	"CSDK-013": {"ASI02"},          // TS SSRF
	"CSDK-014": {"ASI02"},          // tool has no description (spec anchor)
	"CSDK-107": {"ASI05"},          // eval/exec/compile on dynamic input
	"CSDK-108": {"ASI02", "ASI05"}, // tool body spawns a subprocess
	// claude_sdk — repo scope
	"CSDK-201": {"ASI03"}, // project default permission mode bypasses approvals
	"CSDK-202": {"ASI03"}, // session permission mode bypasses approvals
	// claude_sdk — subagent scope
	"CSDK-110": {"ASI03", "AST03"}, // subagent granted Bash (spec anchor)
	"CSDK-111": {"ASI03", "AST03"}, // subagent fs-write/web-fetch built-ins

	// openai_sdk — agent scope
	"OAI-101": {"ASI01"},          // no input_guardrails + shell/fs tools
	"OAI-102": {"ASI02"},          // tool_use_behavior stop_on_first_tool
	"OAI-103": {"ASI08"},          // forced tool_choice without reset (loop risk)
	"OAI-105": {"ASI01"},          // TS content-fetching hosted tool, no inputGuardrails
	"OAI-106": {"ASI01", "ASI04"}, // MCP servers without input_guardrails
	"OAI-109": {"ASI01"},          // WebSearchTool without input_guardrails
	"OAI-111": {"ASI03"},          // privileged hosted tool without needs_approval
	// openai_sdk — tool scope
	"OAI-006": {"ASI02"},          // path accepted without normalization
	"OAI-012": {"ASI02", "ASI05"}, // tool body spawns a subprocess
	"OAI-013": {"ASI05"},          // eval/exec/compile on dynamic input
	"OAI-014": {"ASI03"},          // privileged tool has no needs_approval gate
	"OAI-017": {"ASI05"},          // TS eval / new Function on dynamic input

	// claude_skill — skill scope
	"CSKILL-001": {"ASI03", "AST03"}, // auto-approves unrestricted shell
	"CSKILL-002": {"ASI05", "AST05"}, // shell during load (dynamic-context exec)
	"CSKILL-003": {"ASI04", "AST05"}, // load-time exec does egress / reads secrets
	"CSKILL-010": {"ASI04"},          // bundled script performs network egress
	"CSKILL-011": {"ASI03", "ASI04"}, // bundled script reads credentials
	"CSKILL-030": {"ASI04"},          // hardcoded secret in bundled file
	"CSKILL-050": {"ASI03", "AST03"}, // model-invocable skill, side-effecting grants
}

// OWASPFor returns the pinned OWASP IDs for a rule, or nil when the rule is
// unmapped (the caller omits the key — never an empty list).
func OWASPFor(ruleID string) []string {
	ids := owaspMap[ruleID]
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}
