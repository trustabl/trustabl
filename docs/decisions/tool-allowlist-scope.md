# Tool allow-list / access-control detection: scope across SDKs

This closes out the design blocker on detecting agents with missing or
overly-broad tool access control. Investigation split the supported SDKs
into three behavior classes, because "empty allow-list" does not mean the
same thing in each one. This doc records the LangChain decision (not
applicable), scopes and partially ships the Claude SDK / OpenAI SDK work
(Class 1), and records Google ADK, the one class that shipped as a real rule
first — see [ADK-111](../../testdata/rules-fixture/google_adk/agent_safety.yaml)
— because ADK is the one SDK where an explicit allow-list genuinely narrows an
otherwise-unbounded tool surface. Claude SDK's repo-scope half of Class 1
shipped next, as **CSDK-205** — see below.

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

**Known gap (per the three-repo model):** this change is in the engine
fixture (`testdata/rules-fixture/`) only, per the scoped ask for this
session. It is not yet mirrored into `trustabl-rules` (production) or
given a rationale doc in `trustabl-rulebook`
(`docs/Policy/google_adk/agent_safety.md`) — both sibling repos exist
locally at `../trustabl-rules` and `../trustabl-rulebook`. Per
`CLAUDE.md`'s sync obligation, ADK-111 is tested but not yet shipped to
users; that mirroring + rationale-doc step is next before this can ship.

## Class 1 — Claude SDK / OpenAI SDK

Claude SDK repo-scope: **shipped as CSDK-205.** Claude SDK agent-scope and the
OpenAI Agents SDK half remain not implemented — see below for each.

### Claude SDK repo-scope: shipped (CSDK-205)

`ClaudeAgentOptionsDef` (`internal/models/agent.go`) already captured every
constructor kwarg generically onto a single `Kwargs *KwargTree` — so
`disallowed_tools` was structurally present in the data whenever source set
it, and no discovery change was needed. `internal/rules/predicates.go`
previously exposed only two repo-scope readers of that struct:
`PredRepoClaudeOptionsPermissionModeIs` (value check on `permission_mode`)
and `PredRepoClaudeOptionsMaxTurnsMissing` (absence check, hardcoded to the
`max_turns` kwarg name).

Delivered:

- Generalized `PredRepoClaudeOptionsMaxTurnsMissing`'s body into a shared
  helper, `repoClaudeOptionsMissingKwarg(inv, kwarg string)`
  (`internal/rules/predicates.go`) — the same shape `agentRunCallMissingKwarg`
  uses for the agent-run-call family. `PredRepoClaudeOptionsMaxTurnsMissing`'s
  observable behavior is unchanged; `PredRepoClaudeOptionsDisallowedToolsMissing`
  is the new reader built on the same helper.
- The standard four-file schema change (`schema.go` + `predicates.go` +
  `evaluator.go` + `schema.yaml`), plus a `schema_version` bump (14 → 15) in
  the fixture and `trustabl-rules` manifests.
- Rule **CSDK-205** in `claude_sdk/repo.yaml` (fixture and production, both
  synced), severity medium / confidence 0.7:
  `repo_claude_options_permission_mode_is: [acceptEdits]` combined (`all:`)
  with `repo_claude_options_disallowed_tools_missing: true`. The mode list is
  `acceptEdits` only, deliberately excluding `bypassPermissions` — CSDK-202
  already owns that value at high/0.9, and including it here would make every
  tripping repo report twice on the same `ClaudeAgentOptions(...)` call.
  `acceptEdits` is the genuinely uncovered surface: file edits auto-approve,
  and `allowed_tools` only auto-approves rather than restricts, so with no
  `disallowed_tools` deny-list nothing bounds the rest of the tool surface.
- Fire/silent cases in `policyRepoRuleCases`
  (`internal/rules/policies_test.go`), including a case exercising the
  asymmetric Opaque-skip between the two combined predicates (permission_mode
  reads Opaque constructions; the missing-kwarg helper skips them).
- Rationale doc updated: `../trustabl-rulebook/docs/Policy/claude_sdk/repo.md`
  gained a CSDK-205 rule-by-rule defense block, and its "what this policy
  does not cover" section — which previously said `acceptEdits` was
  deliberately not flagged — was corrected to say only *bare* `acceptEdits`
  (with a deny-list present) goes unflagged.

**Known gap carried forward, not fixed:** the presence check underlying
`repoClaudeOptionsMissingKwarg` only requires `node.Value != nil`, so
`disallowed_tools=[]` (empty list) and `disallowed_tools=None` both read as
"set" and silence CSDK-205 — the same tri-state gap `max_turns=None` already
had for CSDK-204. Documented in the rulebook's confidence-gap section rather
than fixed, since tightening it would also change CSDK-204's long-shipped
behavior.

### Claude SDK agent-scope: scoped, not yet built

There are two separate places `permission_mode` / `disallowed_tools` can
appear in a Claude SDK codebase; the repo-scope one (`ClaudeAgentOptions(...)`
session config) is now covered by CSDK-205 above. The agent-scope one remains
scoped but unbuilt:

**`AgentDefinition(...)` in Python, or a `query(...)` main-agent's inline
`options` in TS.** These constructors' kwargs land on `AgentDef.Kwargs`
generically, via the same `extractCallKwargs` mechanism every other SDK's
kwargs go through (`internal/analysis/agents.go`). This is confirmed already
exercised: `testdata/rules-fixture/claude_sdk/agent_safety.yaml` already has
a rule matching
`agent_kwarg_value: {kwarg: permissionMode, value: bypassPermissions}`,
and separate rules already read `agent_grants_builtin_tool` against
`options.allowedTools` directly. For the TS `query(...)` main agent
specifically, `internal/analysis/ts_agents.go:93-94` captures the whole
options object as a nested `KwargTree` when options are inline
(`agent.Kwargs = astutil.TSObjectKwargs(root, pf.Source)`), so
`options.permissionMode` and `options.disallowedTools` are reachable via
dotted-path lookup the same way.

**This means the agent-scope rule needs no new discovery or schema field.**
It's buildable today, purely as a new rule, from the already-existing generic
predicates:

```yaml
match:
  all:
    - agent_kwarg_value: {kwarg: permissionMode, value: acceptEdits}  # not bypassPermissions — mirror the CSDK-205 reasoning: that value already has its own rule
    - agent_kwarg_missing: [disallowedTools]
```

(Python `AgentDefinition` would use `permission_mode`/`disallowed_tools`;
same combinator, different kwarg spelling.) This is a separate rule from
CSDK-205, not a mirror of it — an agent's own `AgentDefinition(...)` posture
and the session-level `ClaudeAgentOptions(...)` posture are two distinct
constructs that can disagree within the same repo.

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
