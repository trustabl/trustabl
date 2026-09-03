# Tool allow-list / access-control detection: scope across SDKs

This closes out the design blocker on detecting agents with missing or
overly-broad tool access control. Investigation split the supported SDKs
into three behavior classes, because "empty allow-list" does not mean the
same thing in each one. This doc records the LangChain decision (not
applicable) and scopes the Claude SDK / OpenAI SDK work (not yet
implemented). Google ADK is the one class that shipped as a real rule —
see [ADK-111](../../testdata/rules-fixture/google_adk/agent_safety.yaml) —
because ADK is the one SDK where an explicit allow-list genuinely narrows an
otherwise-unbounded tool surface.

## Class 3 — LangChain: no permission-model concept (not applicable)

LangChain agent discovery (`internal/analysis/langchain_agents.go`,
`internal/analysis/ts_langchain_agents.go`, `internal/analysis/langgraph_graph.go`)
sets fields on the shared `models.AgentDef` — `SDK`, `Class`, `Language`,
`Location`, `Kwargs`, `Opaque`, and (when resolvable) `Name`/`VarName`. A
grep of all five LangChain discovery files (`langchain_agents.go`,
`langchain_tools.go`, `langchain_hosted_tools.go`, `ts_langchain_agents.go`,
`ts_langchain_tools.go`) for `allow|deny|permission|allowed_tools|blocked`
returns zero matches. `AgentDef` itself has no allow-list, deny-list, or
permission-mode field for any SDK — it carries `ToolRefs`/`HostedToolRefs`
(what's wired in), not a policy over what's wired in.

For LangChain, `tools=` (Python `create_react_agent`/`create_agent`/
`AgentExecutor`) or `tools:` (TS) is simply the literal, complete set of
tools the agent can call — there is no separate "allow-list that narrows a
larger set" layer, no `permission_mode`, and no `disallowed_tools`
concept anywhere in the SDK's surface as discovered by this codebase. An
agent with `tools=[]` has zero tools, full stop; there is nothing else
implicitly available for an allow-list to have restricted. "Empty = no
tools" is not a security finding — it's just an inert agent.

**Decision: this class of rule does not apply to LangChain.** The
`google_adk/agent_safety.yaml` shape (fire when an allow-list-bearing
construct has no explicit allow-list) has no LangChain analogue to attach
to. `testdata/rules-fixture/langchain/agent_safety.yaml` already has the
one LangChain rule that *is* the right shape for this SDK — LC-101 flags
an agent that wires a code-execution/shell built-in tool directly (the
risk is which tools are wired, not whether a filter narrows a wider set).

## Class 2 — Google ADK: shipped (ADK-111)

Investigation initially assumed a dedicated ADK tool-allow-list field
existed on `AgentDef` to confirm. It does not: ADK's `tools=` kwarg is
captured generically via `Kwargs.Children["tools"]`, same as every other
constructor kwarg, and an absent/empty `tools=` list yields zero
`ToolRefs` — the most restrictive state, not the least. So the original
framing ("empty `tools=` is unrestricted") does not hold structurally for
plain `tools=`.

The real Class-2 mechanism is narrower and more specific:
[`MCPToolset`](https://github.com/google/adk-python) — an ADK tool that
connects an agent to a whole MCP server's tool catalog. Its `tool_filter=`
kwarg is the actual allow-list: set, it narrows the agent to the named
tools; unset, the agent gets every tool the remote server currently
exposes — a surface that isn't enumerable from the agent's source, can
grow whenever the server changes, and is outside this codebase's control.
That's a genuine "empty allow-list = unrestricted" case.

`MCPToolset` was not previously in `ADKHostedToolClasses`
(`internal/analysis/adk_hosted_tools.go`), so a `tools=[MCPToolset(...)]`
item fell through classification and became an opaque External `ToolRef`
— its kwargs, including `tool_filter`, were discarded entirely. Delivered:

- Added `MCPToolset` to `ADKHostedToolClasses` — the one discovery change
  needed. Every call already has its keyword arguments captured onto
  `Expr.CallKwargs` regardless of classification
  (`internal/analysis/agents.go:734-740`); recognizing the class just
  routes those kwargs onto a queryable `HostedToolDef.Kwargs` instead of
  discarding them.
- Rule **ADK-111** in `testdata/rules-fixture/google_adk/agent_safety.yaml`,
  built entirely from existing predicates —
  `agent_uses_hosted_tool_class: [MCPToolset]` and
  `not: agent_hosted_tool_kwarg_present: {class: MCPToolset, kwarg: tool_filter}`
  — no new predicate, schema field, or `evaluator.go` wiring was needed.
- Fire/silent cases added to `policyAgentRuleCases` in
  `internal/rules/policies_test.go`; `TestPolicyRules_AllRulesCovered`
  passes. `go build ./...` and `go test ./...` are clean.

**Known gap (per the three-repo model):** this change was initially
scoped to the engine fixture only. It has since been mirrored into
`trustabl-rules` (production) via
[trustabl-rules#51](https://github.com/trustabl/trustabl-rules/pull/51)
and given a rationale doc in `trustabl-rulebook`
(`docs/Policy/google_adk/agent_safety.md`), so ADK-111 is fully
shipped per `CLAUDE.md`'s sync obligation — engine, rules, and
rulebook are all in sync.

## Class 1 — Claude SDK / OpenAI SDK: scoping only (not implemented)

Per instruction, no predicate or rule was written for this class this
session. What follows is what's confirmed present vs. missing, and the
smallest concrete change that would make the "no `disallowed_tools` AND a
permissive/bypassing `permission_mode`" signal detectable.

### Claude SDK: closer than expected — mostly already wired

There are two separate places `permission_mode` / `disallowed_tools` can
appear in a Claude SDK codebase, and they're captured very differently:

**1. Agent-scope (`AgentDefinition(...)` in Python, or a `query(...)`
main-agent's inline `options` in TS).** These constructors' kwargs land on
`AgentDef.Kwargs` generically, via the same `extractCallKwargs` mechanism
every other SDK's kwargs go through (`internal/analysis/agents.go`). This
is confirmed already exercised: `testdata/rules-fixture/claude_sdk/agent_safety.yaml`
already has a rule matching
`agent_kwarg_value: {kwarg: permissionMode, value: bypassPermissions}`,
and separate rules already read `agent_grants_builtin_tool` against
`options.allowedTools` directly. For the TS `query(...)` main agent
specifically, `internal/analysis/ts_agents.go:93-94` captures the whole
options object as a nested `KwargTree` when options are inline
(`agent.Kwargs = astutil.TSObjectKwargs(root, pf.Source)`), so
`options.permissionMode` and `options.disallowedTools` are reachable via
dotted-path lookup the same way.

**This means the Class-1 agent-scope rule needs no new discovery or
schema field.** It's buildable today, purely as a new rule, from the
already-existing generic predicates:

```yaml
match:
  all:
    - agent_kwarg_value: {kwarg: permissionMode, value: bypassPermissions}  # or the accept-edits mode, whichever counts as "permissive" — needs a product decision on the exact mode list
    - agent_kwarg_missing: [disallowedTools]
```

(Python `AgentDefinition` would use `permission_mode`/`disallowed_tools`;
same combinator, different kwarg spelling.)

**2. Repo-scope (`ClaudeAgentOptions(...)` session config, not an
agent).** `internal/analysis/claude_agent_options.go`'s
`DiscoverClaudeAgentOptions` also captures every kwarg generically onto
`ClaudeAgentOptionsDef.Kwargs` — so `disallowed_tools` is structurally
present in the data today whenever the source sets it. But
`internal/rules/predicates.go` currently exposes only two repo-scope
readers of that struct: `PredRepoClaudeOptionsPermissionModeIs` (value
check on `permission_mode`) and `PredRepoClaudeOptionsMaxTurnsMissing`
(absence check, but hardcoded to the `max_turns` kwarg name). **There is
no existing predicate reading `disallowed_tools` absence at repo scope.**

The smallest fix: a new repo-scope predicate mirroring
`PredRepoClaudeOptionsMaxTurnsMissing`'s exact shape
(`internal/rules/predicates.go:1158-1176` — skip `Opaque` constructions,
fire only if at least one concrete `ClaudeAgentOptions(...)` exists and
none set the kwarg), parameterized on `disallowed_tools` instead of
`max_turns` — or, better, generalize `PredRepoClaudeOptionsMaxTurnsMissing`
into a `repoClaudeOptionsMissingKwarg(inv, kwarg string)` helper (the same
shape `agentRunCallMissingKwarg` already uses for the agent-run-call
family) so both `max_turns` and `disallowed_tools` share one
implementation. That's the standard four-file schema change
(`schema.go` + `predicates.go` + `evaluator.go` + `schema.yaml`) the
fixture's `CLAUDE.md` documents, plus a `schema_version` bump in both the
fixture and `trustabl-rules` manifests.

### OpenAI Agents SDK: researched — mis-filed as Class 1, real mechanisms are Class-2-shaped

The prior session's grep of this codebase's own discovery files found only
per-tool `needs_approval` (OAI-111) and correctly flagged the OpenAI half as
genuinely unresearched rather than guessing. This session read the actual
`openai-agents-python` / `openai-agents-js` source and docs (not just how
other SDKs work) to close that gap. Verdict: **a session/run-level
permission model is confirmed absent, for an architectural reason** — but
**two real registry-wide allow-list mechanisms exist**, missed by the
earlier grep because they're keyed on `tool_filter` / `allowed_tools`, not
`permission_mode` / `disallowed_tools` / `RunConfig`. Both are closer in
shape to ADK's `MCPToolset(tool_filter=)` (Class 2) than to Claude's session
config (Class 1) — the original three-class grouping mis-filed OpenAI.

**Confirmed absent: no run-level or session-level permission model.**
`RunConfig` (`src/agents/run_config.py`, all 25 fields read directly) has no
`allowed_tools`, `disallowed_tools`, or `permission_mode` — its only
tool-adjacent fields (`tool_error_formatter`, `tool_not_found_behavior`,
`tool_name_collision_policy`, `tool_execution: ToolExecutionConfig`) are
execution/error-shaping knobs, not authorization. `Runner.run` /
`run_sync` / `run_streamed` take no permission parameter. And the
`Runner.run(..., hooks=...)` approval-hook this doc previously hypothesized
as the most likely candidate **does not exist**: `RunHooks.on_tool_start`
and `on_tool_end` (`src/agents/lifecycle.py`) both return `None` — they are
observational, with no way to veto or filter a tool call. That earlier
guess is retracted here.

The architectural reason: an OpenAI agent starts with **zero** tools, and
`Agent(tools=[...])` is the complete, enumerable surface — the same
situation this doc already reasoned through for LangChain (Class 3) and
for ADK's plain `tools=`. Claude's `allowed_tools` / `permission_mode`
exist only because a Claude session starts with a broad built-in tool set
already available to narrow. OpenAI has no such implicit default, so
"absent `tools=`" is the *most* restrictive state, not the least, and OAI's
per-tool `needs_approval` is not the tip of a session-wide iceberg — it's
the whole per-tool story.

**Real mechanism 1 — MCP server `tool_filter`.** `src/agents/mcp/util.py`
defines `ToolFilterStatic{allowed_tool_names, blocked_tool_names}`;
`ToolFilter = ToolFilterCallable | ToolFilterStatic | None`, and when
`None`, **no filtering occurs — the agent gets every tool the remote MCP
server currently exposes**, not enumerable from source, able to grow when
the server changes. `create_static_tool_filter(...)` returns `None` when
both lists are `None`. Used as
`MCPServerStdio(params={...}, tool_filter=create_static_tool_filter(
allowed_tool_names=[...]))`. TS confirms the same shape:
`MCPServerStdio({ toolFilter })` + `createMCPToolStaticFilter({ allowed,
blocked })` (`MCPToolFilterStatic`). This is structurally identical to
ADK-111's `MCPToolset(tool_filter=)` — "absent = unbounded, risky" is
unambiguous.

Discovery gap: **Python discards it.** `classifyMCPServerCall`
(`internal/analysis/mcp_servers.go:33-58`) has an explicit comment —
*"Kwargs intentionally not captured at v1"* — leaving `MCPServerDef.Kwargs`
nil even though the field exists (`internal/models/agent.go:141`). **TS
already captures it** (`ts_openai_mcp_servers.go:82`,
`def.Kwargs = astutil.TSObjectKwargs(...)`). But no predicate anywhere
reads `MCPServerDef` at all — a grep of `internal/rules/predicates.go` for
`MCPServer`/`mcp_server` returns zero hits; the existing OAI-106 only reads
the agent's generic `mcp_servers` kwarg presence, never the resolved
`MCPServerDef`s. This needs **both** a discovery change (populate Python
`MCPServerDef.Kwargs`, same move as ADK-111's `Expr.CallKwargs` → queryable
struct) **and** a new MCP-server-scoped predicate family — the standard
four-file schema change plus a `schema_version` bump in both the fixture
and `trustabl-rules`. Not a rules-only change.

**Real mechanism 2 — `HostedMCPTool.tool_config.allowed_tools`.**
`HostedMCPTool` (`src/agents/tool.py`) wraps `tool_config: Mcp`, a raw
pass-through of the Responses API `Mcp` TypedDict
(`openai/types/responses/tool_param.py`). `allowed_tools:
Optional[McpAllowedTools]` — *"List of allowed tool names or a filter
object"* — bounds which tools of the remote server the model can see, and
is orthogonal to `require_approval` (which only gates human confirmation).
Absent `allowed_tools` = the model gets the server's entire catalogue —
same "risky absence" shape as mechanism 1, but for the *hosted* MCP path.

Unlike mechanism 1, **this one is reachable in Python with zero engine
changes**: `HostedMCPTool` is already in `HostedToolClasses`
(`internal/analysis/hosted_tools.go:17`); Python dict literals already
recurse into nested `KwargTree` children
(`internal/analysis/agents.go:750`, `dictChildren`), so
`tool_config={"allowed_tools": [...]}` is already a reachable leaf; and
`HostedToolKwargExpr.Kwarg` is documented dotted-path-capable
(`internal/rules/schema.go:141`), resolved by the existing
`PredAgentHostedToolKwargPresent` (`internal/rules/predicates.go:735-745`).
A rule of the ADK-111 shape — `agent_uses_hosted_tool_class:
[HostedMCPTool]` combined with `not: agent_hosted_tool_kwarg_present:
{class: HostedMCPTool, kwarg: tool_config.allowed_tools}` — is buildable
today as a **rules-only change**, no schema/predicate/evaluator work, no
`schema_version` bump. This is the cheapest available win of the three
candidates here. TS caveat: `classifyTSOpenAIHostedFactoryCall`
(`internal/analysis/ts_openai_hosted_tools.go:41-65`) never sets `Kwargs`
at all, so `hostedMcpTool({...})`'s options are invisible on the TS path
until that's added — such a rule would ship Python-only at first.

**The absent-semantic nuance (OpenAI's version of Claude's
auto-approve-vs-restrict trap):** `require_approval` on `HostedMCPTool`
diverges *by language* for an identical source-level omission:

- **TypeScript**: `hostedMcpTool()` (`packages/agents-core/src/tool.ts`)
  injects `require_approval: 'never'` when the option is omitted — stated
  directly in source and restated in the official JS MCP guide
  ("`requireApproval` … Defaults to `'never'`"). Omission is **risky**.
- **Python**: `tool_config` is passed through raw with no default
  injection by the SDK, so omission falls through to the Responses API's
  platform default, which OpenAI's API guide states in prose as
  approval-required ("By default, OpenAI will request your approval before
  any data is shared with a connector or remote MCP server"). Omission is
  **safe** — but this is a *platform* default stated in docs, not pinned by
  any quotable line of SDK source, and is therefore weaker evidence than
  the TS finding and subject to change server-side without an SDK version
  bump. Any future rule on `require_approval` must be language-gated (two
  rules, per `CLAUDE.md`'s SDK-scoped-rules discipline) — and should be
  scoped to TS first, where the default is verifiable in code.

| Construct | Absent means | Semantic |
|---|---|---|
| `Agent(tools=[...])` | zero tools | safe — no rule (same as LangChain / plain ADK `tools=`) |
| MCP `tool_filter` / `toolFilter` | server's full catalogue | risky — ADK-111 shape (mechanism 1) |
| `HostedMCPTool.tool_config.allowed_tools` | server's full catalogue | risky — ADK-111 shape (mechanism 2) |
| `HostedMCPTool.tool_config.require_approval` | TS: never-approve; Python: platform default approve | risky in TS, safe in Python — language-gated |
| `function_tool(needs_approval=)` | `False` | risky — already shipped as OAI-111 |
| `function_tool(is_enabled=)` | `True` | not a security gate — dynamic enablement, ignore |

**Not implemented this session** (research-only, per instruction). Next
session can pick up directly, in cost order: (1) `HostedMCPTool`
`allowed_tools` rule — rules-only; (2) TS hosted-tool kwarg capture, which
unblocks both the TS `allowedTools` rule and the TS `require_approval:
'never'` rule; (3) MCP `tool_filter` — discovery + new predicate family +
schema bump.
