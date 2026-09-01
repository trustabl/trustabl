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

### OpenAI Agents SDK: not actually scoped yet — needs its own investigation

This is the part of Class 1 that is genuinely unresearched, not just
unimplemented. A grep across every OpenAI-touching discovery file
(`internal/analysis/agents.go`, `agent_run_calls.go`, `hosted_tools.go`,
`ts_openai_agents.go`, `ts_openai_hosted_tools.go`, `mcp_servers.go`,
`ts_openai_mcp_servers.go`) for `needs_approval|tool_use_behavior|
RunConfig|approval` finds exactly one relevant hit: `needs_approval` as a
**per-hosted-tool** kwarg (`ShellTool(needs_approval=False)`,
`LocalShellTool`, `CodeInterpreterTool`, `ApplyPatchTool` — already
covered by the shipped rule OAI-111). There is no discovered concept in
this codebase resembling a session- or agent-level `permission_mode` or
`disallowed_tools` for the OpenAI Agents SDK — nothing analogous to
`ClaudeAgentOptions` exists in the inventory today.

Before proposing fields for this half, the open question is whether the
`openai-agents` SDK itself has a real mechanism matching the "auto-approve
list that doesn't restrict" shape at all (a `RunConfig`-level approval
hook is the most likely candidate, `Runner.run(..., hooks=...)` or
similar), or whether the original Class 1 grouping conflated OpenAI's
per-tool `needs_approval` (already handled by OAI-111) with a session-wide
concept that doesn't exist in this SDK. That's a scoping question for the
next session, not a "smallest fix" like the Claude SDK half above — it
needs the same kind of `openai-agents` source reading that grounded the
ADK `MCPToolset` finding here, before any schema work is proposed.
