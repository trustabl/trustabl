# Instructions for Claude — detection policies

These instructions apply to any work on the detection rule packs. Production
rules live in the external `trustabl-rules` repository
(`https://github.com/trustabl/trustabl-rules`); `testdata/rules-fixture/`
(this directory) is an in-engine **test mirror** of those packs, and **must be
kept in sync with the live repo** — see the "Two-repo rule model" section in
the top-level [`CLAUDE.md`](../../CLAUDE.md) for the full change workflow. The
`../` relative links below refer to the engine's `internal/rules/` package —
from this fixture location that is `../../internal/rules/`.

## Required reading order before editing

1. [`../schema.yaml`](../schema.yaml) — authoritative field reference.
2. [`README.md`](README.md) — author conventions in this directory.
3. The closest existing rule to what's being asked for — pattern example.

Do not skip step 1.

## Hard rules

- **Never invent YAML keys.** The schema is closed
  (`KnownFields(true)`). If a field you want does not exist in
  [`../schema.go`](../schema.go), extending the schema is a four-file
  change (schema.go + predicates.go + evaluator.go + schema.yaml). Make
  all four changes in one commit.
- **Never change a rule's `id` after it has shipped.** IDs are external
  identifiers; downstream consumers cite them.
- **Never duplicate a rule ID across files.** The loader rejects this at
  startup; per-rule tests catch it faster — run `go test ./...`.
- **Never widen `applies_to` across SDKs casually.** A rule's
  `explanation` / `fix` text is usually SDK-specific. Adding
  `openai_tool` to a Claude-SDK rule (or vice versa) makes the user-facing
  text lie. If a cross-SDK pattern is genuinely needed, author a separate
  rule under that SDK's category (`policies/<sdk>_sdk/<topic>.yaml`) with
  framing that matches the target SDK.
- **Never silence the per-rule test suite** in
  [`policies_test.go`](../policies_test.go). The
  `TestPolicyRules_AllRulesCovered` test fails when a shipped rule has no
  fire/silent case in `policyRuleCases`; that's the contract that keeps
  every shipped rule honest. Add the test cases, don't remove the check.
- **Never write rules at `info` severity.** Reserved.

## Required fields per rule

Every rule MUST set: `id`, `title`, `scope`, `severity`, `confidence`,
`applies_to`, `match`, `explanation`, `fix`. The loader refuses to start
the scanner if any are missing (`scope` empty is rejected with "scope is
required") — this surfaces as a `scan: ...` error in the CLI.

`language:` is OPTIONAL but state it explicitly — defaults to `python`
when omitted. For TypeScript / JavaScript / Go rules, set it explicitly:
`language: typescript`. Python and TypeScript discovery are both plumbed in
today (Claude SDK, OpenAI Agents, Google ADK, and MCP all have TS discovery
and shipped TS rules). JavaScript (`.js`/`.jsx`/`.mjs`/`.cjs`) now shares the
TypeScript grammar and discovery (the tsx parser parses plain JS) and is
audited by the `language: typescript` rule packs via the TS/JS family gate
(`models.IsTSOrJS`) — so a `typescript` rule fires on `.js` too (and a
`javascript` rule, were one shipped, fires on both). Go is still recognized by
recon but has no AST parser, so `go` rules load but never fire until one ships.

## Per-scope `applies_to` values

The `applies_to` list constrains which discovered entities a rule fires
against. Pick values from the table for the scope you're targeting.

**`scope: tool`** — receives a `ToolDef`; `applies_to` is matched against
`ToolDef.Kind`:

| `applies_to` value   | Matches                                       |
| -------------------- | --------------------------------------------- |
| `openai_tool`        | `@function_tool`-decorated Python function    |
| `claude_sdk_tool`    | `@tool` / `@claude_tool` / `claude_agent_sdk` |
| `mcp_tool`           | `@server.tool`, `@mcp.tool`, `.register_tool` |
| `shell_invocation`   | Bare function that calls `subprocess.*` etc. (no rules currently target this — OSH-* moved to a closed-source project) |
| `adk_function_tool`  | `FunctionTool(fn)` wrapping a Python function (Google ADK) |

**`scope: agent`** — receives an `AgentDef`; `applies_to` is matched against
`AgentDef.Class` + `AgentDef.SDK`:

| `applies_to` value        | Matches                                                       |
| ------------------------- | ------------------------------------------------------------- |
| `openai_agent`            | `Agent(...)` from `openai-agents` SDK                         |
| `openai_sandbox_agent`    | `SandboxAgent(...)` from `openai-agents` SDK                  |
| `claude_agent_definition` | `AgentDefinition(...)` from `claude-agent-sdk`                |
| `claude_query_main`       | `query(...)` main-thread agent (`QueryMainAgent`) from the Claude TS SDK |
| `adk_llm_agent`           | `LlmAgent(...)` / `Agent(...)` alias from `google-adk`        |
| `adk_sequential_agent`    | `SequentialAgent(...)` from `google-adk`                      |
| `adk_parallel_agent`      | `ParallelAgent(...)` from `google-adk`                        |
| `adk_loop_agent`          | `LoopAgent(...)` from `google-adk`                            |
| `adk_langgraph_agent`     | `LanggraphAgent(...)` from `google-adk`                       |

**`scope: repo`** — receives `RepoProfile` + `RepoInventory`. `applies_to`
at this scope is matched against a fixed token list (the loader's
`validAppliesToForScope`); these tokens are *category-like* labels, not the
SDK enum values used by the `repo_has_sdk_in_code` predicate:

| `applies_to` value | Matches repos that use               |
| ------------------ | ------------------------------------ |
| `claude_sdk`       | Claude Agent SDK                     |
| `openai_agents`    | OpenAI Agents SDK                    |
| `openshell`        | NVIDIA OpenShell SDK (no rules currently target this — OSH-* moved to a closed-source project) |
| `mcp`              | Model Context Protocol               |
| `google_adk`       | Google ADK (Python)                  |

Repo-scope rules typically combine `applies_to` with a `repo_has_sdk_in_code`
predicate to narrow firing to repos that actually use the SDK in code (e.g.
OAI-201's `match: { all: [ repo_has_sdk_in_code: [openai_agents], ... ] }`).
The two tokens look similar but live in different namespaces — `applies_to`
accepts `openai_agents`, and `repo_has_sdk_in_code` also accepts `openai_agents`
(the SDK enum, `models.SDKOpenAIAgents`); for Claude, `applies_to` uses
`claude_sdk` (the category) while `repo_has_sdk_in_code` uses `claude_agent_sdk`
(the SDK enum). Mismatching the two will silently fail the loader's scope
check.

Always set `applies_to` explicitly — the loader does not infer scope from
the category. Omitting it would make a rule fire against every entity of
that scope regardless of SDK, which is almost never correct.

**`scope: subagent`** — receives a `SubagentDef` (one `.claude/agents/*.md`
frontmatter declaration, matched at any path depth). `applies_to` is matched
against a fixed token:

| `applies_to` value | Matches                                                         |
| ------------------ | --------------------------------------------------------------- |
| `claude_subagent`  | A `.claude/agents/*.md` subagent declaration (any path depth)   |

Subagent rules use the `subagent_grants_tool` predicate (true when
`SubagentDef.Tools` contains a listed tool name). They carry **no `language:`
field** — subagents are markdown frontmatter, not code, and the
`subagentRuleDetector.Applies` method does not gate on language. The shipped
rules are CSDK-110 ("Subagent granted the built-in Bash tool") and CSDK-111
("Subagent granted filesystem-write or web-fetch built-ins"), both in
`claude_sdk/subagent_safety.yaml`.

## "Add a rule for X"

Default sequence:

1. Read the structurally-closest existing rule (e.g. for a new network
   rule, read `claude_sdk/network.yaml`).
2. Decide whether existing predicates cover the case. Existing first; new
   primitives are a real cost.
3. Pick the category subdirectory and topic file. If neither matches, ASK
   the user before creating new ones.
4. Write the rule. Match the explanation/fix tone of nearby rules — a
   paragraph, plain language, names the consequence and the fix concretely.
5. Add at minimum one fire case AND one silent case to `policyRuleCases`
   in [`../policies_test.go`](../policies_test.go). The
   `TestPolicyRules_AllRulesCovered` guard requires every shipped rule to
   appear in this table.
6. Run `go test ./...`.

The `testdata/corpus/` directory is a real-agent corpus, not a controlled
fixture. The scanner sweep over `testdata/corpus/*` only asserts no crash — it
does NOT assert that any specific rule fires there. Per-rule fire/silent
correctness is the job of `policies_test.go`.

## "Remove a rule"

1. Delete the rule entry from its YAML file. If the file is now empty,
   delete the file.
2. Remove all matching cases from `policyRuleCases` in
   [`../policies_test.go`](../policies_test.go).
3. Run `go test ./...`.

## "Change a rule's severity / confidence / explanation"

In-place edit. No fixture changes required. Run `go test ./...` — the
smoke test only asserts that rules fire, not what severity they fire at.

## When the per-rule test fails after you add a rule

[`../policies_test.go`](../policies_test.go) drives a fire/silent case
table for every shipped rule. Two failure modes and what they mean:

1. **`TestPolicyRules / "RULE-ID fires on ..."` failed** — your rule
   matched a snippet that should have triggered it but didn't. Walk the
   `match:` expression against the snippet by hand; usually a predicate
   set to `true`/`false` is inverted, or `applies_to` is missing the
   ToolKind your test uses.
2. **`TestPolicyRules_AllRulesCovered` failed** with your rule ID listed
   as missing — you added a rule but no entry in `policyRuleCases`. Fix
   by adding at least one fire case and one silent case for that rule ID.

The corpus sweep in
[`../../scanner/scanner_test.go`](../../scanner/scanner_test.go) only
checks the scanner doesn't crash; it does not assert findings. Don't
expect rule failures to surface there.

## Path to production-grade (known gaps)

The shipped pack is calibrated as "signal to investigate," not an
authoritative gate. The highest-leverage work to move it toward
production-grade, in priority order:

- **(a) Value-aware `timeout` check — DONE.** `call_without_kwarg` now treats a
  kwarg present with literal `None` as missing, so `requests.get(url,
  timeout=None)` fires. Same for `agent_kwarg_missing` (e.g.
  `before_tool_callback=None`).
- **(b) Local client-alias matching — DONE (same-function scope).**
  `call_without_kwarg` and `has_dynamic_url_call` resolve local-variable client
  aliases (`s = requests.Session(); s.get(...)`, and the `with ... as c:` form)
  via `analysis.ResolveClientAliases` + `IsHTTPCallNode`. **Residual
  limitations:** (1) instance attributes (`self.client.get(...)`) and
  cross-function / cross-module aliases are still unresolved; (2) the engine
  canonicalizes an aiohttp session alias to `aiohttp.<method>` (e.g.
  `aiohttp.get`), but rule callee lists currently use
  `aiohttp.ClientSession.<method>` — so aiohttp aliasing is wired in the engine
  but a rule pack must list `aiohttp.<method>` callees to benefit. `requests`
  and `httpx` aliasing work end-to-end with existing callee lists.
- **(c) Hosted + decorated shell tools in the agent shell-tool rules — DONE.**
  `agent_uses_tool_kind: [shell_invocation]` (OAI-101, OAI-104) now matches
  three shapes, not just a bare undecorated function: (1) a bare
  `KindShellInvocation` function; (2) the SDK's hosted shell tools
  (`ShellTool`, `LocalShellTool`, `CodeInterpreterTool`, `ApplyPatchTool`,
  ADK `BashTool`) via the `hostedClassToKind` map over `HostedToolRefs`; and
  (3) a *decorated* tool (`@function_tool` / `tool()`) that shells out —
  discovery stamps a structural `shells_out` fact on the `ToolDef` (Python in
  `buildTool`, TS in `tsHandlerFacts`) and `PredAgentUsesToolKind` honors it
  for `shell_invocation`. Residual: the agent rules still only key on shell
  reach, not the broader "filesystem-touching" half their titles also name.
- **(d) Source-level fire/silent fixtures.** Per-rule cases in
  `policies_test.go` feed hand-constructed typed inputs, so they prove
  predicate logic but not discovery → detection end-to-end. Add fixtures
  that run real `.py` snippets through the full scanner, so a discovery
  change that stops producing a shape a rule depends on fails a test
  instead of silently killing the rule.

## Output discipline for explanation/fix text

These strings are user-facing — they appear in the CLI's scan summary (and
JSON output) and guide whether a user acts on the finding. Vague text
undermines the product.

- **explanation**: name the consequence, not just the pattern. "Returns
  exceptions to the model as opaque strings, so it can't recover" beats
  "raises exceptions".
- **fix**: prescribe the concrete change. "Pass `timeout=` (5–30s)" beats
  "add error handling".
- Match the tone of the surrounding rules — paragraph, no headers, no
  bullets.

## What this directory is NOT

- Not a place for ad-hoc YAML files. Every `.yaml` file here is loaded as
  a policy. Don't drop notes, examples, or work-in-progress files inline.
- Not a doc directory. The README.md and this CLAUDE.md are exceptions;
  they are filtered out by the loader's `.yaml`-suffix check, but the
  convention is "data only, two markdown docs only".
