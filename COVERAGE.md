# Coverage

Coverage matrix for Trustabl's static analysis: which agent SDKs (and which
languages) we currently scan, analyse, and detect against. This file is the
at-a-glance reference; `ARCHITECTURE.md` has the implementation detail.

_Last reviewed: 2026-06-02 (HEAD `990b60d`)._

> **Note:** Detection rules are not shipped in the binary. They live in the
> separate `trustabl-rules` git repository
> (`https://github.com/trustabl/trustabl-rules`) and are resolved at scan
> time (cached locally, with offline fallback). The rule IDs and packs listed
> below describe the rules Trustabl currently ships in that repository; the SDK
> and language *recognition* surface (scanning + AST discovery) is what the
> engine binary provides.

## Coverage matrix

Legend: ✅ full · ◐ partial · ❌ none · — N/A

| SDK | Language | Scanning | Analysis (AST discovery) | Detection rules |
|---|---|---|---|---|
| **Claude Agent SDK** | Python | ✅ dep-scan + file inventory + `.claude/` & `.claude-plugin/` components | ✅ tools, agents, subagents (canonical + flat-collection shape fallback), skills (`SKILL.md`), slash commands, plugin manifests, settings, `ClaudeAgentOptions` session config | ✅ tool CSDK-001..008, 107, 108 (008 = SSRF / caller-controlled URL); agent CSDK-101..105; subagent CSDK-110, 111 (fire on pure-markdown collections); repo CSDK-201, 202 (settings.json `defaultMode` + `ClaudeAgentOptions` `permission_mode` bypass) |
| **Claude Agent SDK** | TypeScript | ✅ dep-scan (`@anthropic-ai/claude-agent-sdk`) + file inventory + `.claude/` components | ✅ tools (`tool()` factory), agents (main thread `QueryMainAgent` per `query()` call + sub-agents inline-in-query + typed-const `AgentDefinition`), MCP servers (createSdkMcpServer + 4 config literals) | ❌ no TS rules yet (SP2) — META-004 fires |
| **OpenAI Agents SDK** | Python | ✅ dep-scan + file inventory | ✅ tools, hosted tools (11 classes), agents, MCP servers (3 transports + alias), guardrails, sessions | ✅ tool OAI-001..016 (016 = SSRF / caller-controlled URL); agent OAI-101..104, 106, 109, 110, 111; repo OAI-201 |
| **OpenAI Agents SDK** | TypeScript | ✅ dep-scan (`@openai/agents` substring catches `-core` / `-openai`) + file inventory | ✅ tools (`tool({...})` factory), agents (`new Agent({...})` + `Agent.create(...)`), hosted tools (9 factories across `@openai/agents-core` and `@openai/agents-openai`), MCP servers (3 transports + `MCPServers` wrapper), guardrails (4 `defineX` factories), sessions (`MemorySession` / `OpenAIConversationsSession` / `OpenAIResponsesCompactionSession`) | ❌ no TS-language OAI rules yet (SP2) — META-004 fires |
| **MCP** | Python | ✅ tool registrations + config files | ◐ tool registrations only (no server-side resource/prompt discovery) | ❌ no dedicated pack (KindMCPTool is reachable by some CSDK rules' `applies_to`) |
| **MCP** | TypeScript / Go / Rust | ❌ no MCP-specific recognition (file paths inventoried generically, no MCP parser or dep needles) | ❌ | ❌ |
| **Google ADK** | Python | ✅ dep-scan (`google-adk`) + file inventory | ✅ LlmAgent (+ Agent alias), SequentialAgent, ParallelAgent, LoopAgent, LanggraphAgent; FunctionTool wrapping; 13 built-in hosted tools; sub_agents edges | ✅ tool ADK-001..007, 009..011 (009 = SSRF, 010 = subprocess, 011 = eval/exec/compile); agent ADK-008, 101..108, 110 |
| **Google ADK** | TypeScript | ✅ dep-scan (`@google/adk`) + file inventory | ✅ tools (`new FunctionTool({...})`), agents (5 constructors: LlmAgent + SequentialAgent + ParallelAgent + LoopAgent + RoutedAgent), hosted tools (13 classes), subAgents edges | ❌ no TS-language ADK rules yet (SP2) — META-004 fires |
| **Google ADK** | Go / Java / Kotlin | ❌ | ❌ | ❌ |
| **OpenShell** | Python | ✅ shell-invocation discovery + `openshell/*.yaml` policy files surfaced | ✅ `KindShellInvocation` tools → `RepoInventory.HasShellInvocations` (the "openshell" risk surface; not an SDK, never in `SDKsDetected`) | ❌ rules moved to closed-source companion project (no rule fires; no META finding — openshell is not treated as an unaudited SDK) |

## What we parse exactly (per SDK)

### Claude Agent SDK — Python

Discovery sources: `internal/analysis/discovery.go`, `agents.go`, `subagents.go`,
`claude_settings.go`.

| Construct | Recognition |
|---|---|
| Tools | Decorators: `@tool`, `@claude_tool`, `@agent.tool`, any decorator containing the substring `claude_agent_sdk`. Captures: function name, params, type annotations, docstring, decorator kwargs |
| Agents | `AgentDefinition(...)` constructor calls. Captures every kwarg into a typed `KwargTree`: `name`, `description`, `prompt`, `tools`, `disallowedTools`, `permissionMode`, `mcpServers`, `skills`, `memory`, `maxTurns`, `background`, `effort`, `initialPrompt`. Typed accessors expose `tools`/`disallowedTools`/`permissionMode`/`mcpServers` without reaching into the tree |
| Subagents | **Hybrid** discovery: canonical `.claude/agents/*.md` (any path depth, monorepo-safe) PLUS a frontmatter-shape fallback over all markdown files (gate: `name` + `tools`/`model`, excluding `SKILL.md` and `.claude/commands/`) that catches flat collections like `VoltAgent/awesome-claude-code-subagents` (subagents under `categories/<NN>/*.md`). Captures `name`, `description`, `tools` (verbatim) + `ToolGrants` (parsed grammar: bare / `Bash(...)` / `mcp__server__tool`), `disallowedTools`, `model`, `permissionMode`, `mcpServers`, `skills`, `isolation`, `HasHooks`. Files without frontmatter or without a `name:` are skipped. Audited by subagent-scope rules — CSDK-110 ("Subagent granted the built-in Bash tool") and CSDK-111 ("Subagent granted filesystem-write or web-fetch built-ins") are the shipped rules; predicate `subagent_grants_tool: [Bash]` matches parsed grants (so `Bash(...)` matches `Bash`). Subagent presence alone marks the repo as `claude_agent_sdk` (no SDK code required), so the pack loads and CSDK-110/111 fire on pure-markdown collections. Subagent rules carry no `language:` field (markdown frontmatter, language-agnostic) |
| Skills | `SKILL.md` (basename, any depth: `.claude/skills/<name>/SKILL.md`, plugin `skills/`, nested). Parses `name`, `description`, `allowed-tools` (space-separated or YAML-list → `ToolGrants`), `argument-hint`, `disable-model-invocation`. No rules target skills yet — surfaced in inventory/JSON |
| Slash commands | **Two path shapes**: canonical `.claude/commands/*.md` AND `<plugin-root>/commands/*.md` whenever `<plugin-root>` has a sibling `.claude-plugin/plugin.json` (the layout used by plugin-distribution repos like `wshobson/agents` — `plugins/<x>/commands/*.md`). Command name = file basename. Parses frontmatter `description`, `allowed-tools`, `model`, `argument-hint`, `disable-model-invocation`; a command without frontmatter is still emitted (body is the prompt). No rules yet — surfaced in inventory/JSON |
| Plugin manifests | `.claude-plugin/plugin.json` and `marketplace.json` JSON-parsed into `PluginManifest` (`kind` plugin/marketplace, `name`, catalog `plugins[]` with `name` + normalized `source`). The `source` field accepts both forms in the wild: a plain string (`"./local-foo"`) or an object (`{"source":"git-subdir","url":"…","path":"…"}` for external git refs); object forms are normalized to `<source>:<url>#<path>` so the trust category survives the round-trip, unknown shapes fall back to raw JSON. A previous typed-string parser dropped the entire manifest on the object form. The recon walk descends into `.claude-plugin/`. No rules yet — surfaced in inventory/JSON |
| Settings | `.claude/settings.json` and `settings.local.json` JSON-parsed: `permissions.allow`/`deny`/`ask` decomposed via the grammar `<Tool>` \| `<Tool>(<pattern>)` plus `mcp__<server>__<tool>`; `defaultMode`, `additionalDirectories`, presence flags for `env`/`hooks`/`sandbox` |
| Session config | `ClaudeAgentOptions(...)` constructor calls. Captures kwargs into a `KwargTree`; `permission_mode` is read by the repo-scope rule CSDK-202. Presence alone marks the repo `claude_agent_sdk` so the pack loads for options-only repos |
| Components surfaced (path-only) | `CLAUDE.md`, `hooks/*.{py,ts,js,jsx,mjs}`, MCP configs (`mcp.json`, `mcp_servers.json`, `claude_desktop_config.json`) |

### Claude Agent SDK — TypeScript

Discovery sources: `internal/analysis/ts_discovery.go`, `ts_agents.go`,
`ts_mcp_servers.go`. Import gate: only files importing from
`@anthropic-ai/claude-agent-sdk` are processed (handles named, renamed,
namespace `* as`, and default imports).

| Construct | Recognition |
|---|---|
| Tools | `tool(name, description, zodSchema, handler, extras?)` factory calls. Captures: name (arg 0), description (arg 1), Zod schema top-level keys as ParamNames, handler body facts (`shells_out`, `http_call`), extras flattened into Config |
| Agents (main thread) | Every `query({prompt, options?})` call emits one `AgentDef` with `Class="QueryMainAgent"`. The TS SDK has no `AgentDefinition` constructor for the main thread — the call site IS the declaration. `Name`: the `const X = query(...)` binding if present, else the enclosing function name (e.g. `"queryStream"`), else `"ClassName.methodName"` if inside a class method. `Opaque=true` when `options` is a computed identifier (the typical real-world shape, e.g. `query({prompt, options: mergedOptions})`); inline `options` populates `Kwargs` plus `ToolRefs` (from `options.allowedTools`) and `MCPServerRefs` (from `options.mcpServers`). When `options` is opaque, `ToolRefs` falls back to a file-scoped scan for any `allowedTools: [...]` array (heuristic — catches the common class-field / named-const shapes; may over-extract if a file has multiple unrelated allowedTools arrays) |
| Agents (sub-agents inline) | Each property inside `query({options: {agents: {...}}})`. Property key becomes `Name`; `Class="AgentDefinition"`; value object becomes `Kwargs` |
| Agents (sub-agents typed-const) | `const x: AgentDefinition = {...}` (and `export const ...`). `Name=VarName=constName`; `Class="AgentDefinition"`; value object becomes `Kwargs` |
| MCP servers | `createSdkMcpServer({...})` → `Class="createSdkMcpServer"`, `Transport="sdk"`. Object literals in `options.mcpServers` discriminated by `type:` → one of `McpStdioServerConfig`/`McpSSEServerConfig`/`McpHttpServerConfig`/`McpSdkServerConfigWithInstance` |
| Tool refs | `agent.tools=["Read","Bash",…]` strings populate `AgentDef.ToolRefs` |
| MCP refs | Each property in `agent.options.mcpServers` populates `AgentDef.MCPServerRefs` (inline-object values → class from `type:`; identifier values → `Class="createSdkMcpServer"`) |

### OpenAI Agents SDK — Python

Discovery sources: `internal/analysis/discovery.go`, `agents.go`, `hosted_tools.go`,
`mcp_servers.go`.

| Construct | Recognition |
|---|---|
| Tools | `@function_tool` decorator with kwargs (`strict_mode`, `failure_error_function`) captured into `ToolDef.Config` |
| Agents | `Agent(...)` and `SandboxAgent(...)` constructor calls. Full KwargTree capture: `instructions`, `model`, `model_settings`, `tools`, `handoffs`, `input_guardrails`, `output_guardrails`, `tool_use_behavior`, `mcp_servers`, `output_type`, `tool_choice`, etc. |
| Hosted tools | Closed set of 11 classes inside `tools=[...]`: `WebSearchTool`, `FileSearchTool`, `ComputerTool`, `HostedMCPTool`, `CodeInterpreterTool`, `ImageGenerationTool`, `LocalShellTool`, `ShellTool`, `ApplyPatchTool`, `CustomTool`, `ToolSearchTool` → emits `HostedToolDef` + `HostedToolRef` edge |
| MCP servers | Closed set of 3 classes inside `mcp_servers=[...]`: `MCPServerStdio` (stdio), `MCPServerSse` (sse), `MCPServerStreamableHttp` (streamable_http). Both inline construction AND `async with X() as srv:` alias resolution (single-file scope) |
| Guardrails | `@input_guardrail` / `@output_guardrail` decorated functions, resolved as edges from each agent. Class-based guardrails are a documented gap |
| Sessions | Construction sites of `SQLiteSession`, `SQLAlchemySession`, `RedisSession`, `MongoDBSession`, `EncryptedSession`, `AdvancedSQLiteSession` |

### OpenAI Agents SDK — TypeScript

Discovery sources: `internal/analysis/ts_openai_tools.go`,
`ts_openai_agents.go`, `ts_openai_hosted_tools.go`,
`ts_openai_mcp_servers.go`, `ts_openai_guardrails.go`,
`ts_openai_sessions.go`, plus the shared `ts_handler_facts.go`. Import
gate: only files importing from `@openai/agents`, `@openai/agents-core`,
or `@openai/agents-openai` (handled by the `TSImportAliasesAny` union
helper) are processed.

| Construct | Recognition |
|---|---|
| Tools | `tool({name, description, parameters, execute, ...})` factory calls. Captures: `name` / `description`, `parameters` top-level keys as `ParamNames`, handler body facts via shared `tsHandlerFacts` (`shells_out`, `http_call`), and option fields (`strict`, `needsApproval`, `timeoutMs`, etc.) flattened into `Config`. `VarName` from the enclosing `const x = tool({...})` binding |
| Agents | `new Agent({...})` and `Agent.create({...})`. All option-object kwargs captured into a typed `KwargTree`; `Opaque=true` when the arg is not an object literal or contains a `...spread`. Pre-resolves hosted-tool factory calls inside `tools: [...]` during discovery; identifier-valued refs in `tools`/`handoffs`/`inputGuardrails`/`outputGuardrails`/`mcpServers` are wired by `ResolveEdges` via a Name+VarName double-indexed lookup |
| Hosted tools | Closed set of 9 factories across `@openai/agents-core` and `@openai/agents-openai`: emits `HostedToolDef` with `SDK=openai_agents` and the canonical factory name |
| MCP servers | `new MCPServerStdio({...})` / `MCPServerSSE` / `MCPServerStreamableHttp` / `MCPServers` (the multi-transport wrapper). Emits `MCPServerDef` with `Transport` ∈ `stdio` / `sse` / `streamable_http` / `multi` and `VarName` from the enclosing `const` |
| Guardrails | `defineInputGuardrail` / `defineOutputGuardrail` / `defineToolInputGuardrail` / `defineToolOutputGuardrail` factory calls. Emits `GuardrailDef` with `Kind` ∈ `input` / `output` / `tool_input` / `tool_output` and `VarName` from the enclosing `const` |
| Sessions | `new MemorySession()`, `new OpenAIConversationsSession()`, `new OpenAIResponsesCompactionSession()`, and the `startOpenAIConversationsSession()` factory. Emits `SessionUse` with `Class` set to the canonical name |

### MCP — Python

| Construct | Recognition |
|---|---|
| Server tool registrations | Decorators: `@server.tool`, `@mcp.tool`, and `.register_tool(...)` calls. Tagged as `KindMCPTool` in inventory |
| Config files | `mcp.json`, `mcp_servers.json`, `claude_desktop_config.json` surfaced as `mcp_config` components (paths only — not deep-parsed) |

### Google ADK — Python

Discovery sources: `internal/analysis/adk_agents.go` (agents and FunctionTool-wrapped
tools), `internal/analysis/adk_hosted_tools.go` (built-in hosted tool classes).
Import gate: only files containing `from google.adk` or `import google.adk` are
processed, which prevents the bare `Agent` class name from colliding with OpenAI's
identically-named class.

| Construct | Recognition |
|---|---|
| Agents | Constructor calls for `LlmAgent`, `SequentialAgent`, `ParallelAgent`, `LoopAgent`, `LanggraphAgent`. The `Agent` alias is recognized and normalized to `LlmAgent` in the emitted `AgentDef.Class`. All constructor kwargs captured into a typed `KwargTree` |
| FunctionTool-wrapped tools | `FunctionTool(symbol)` calls where the argument resolves to a same-file top-level function → emits a `ToolDef` with `Kind=adk_function_tool`. Cross-module resolution is out of scope |
| Built-in hosted tools | Closed set of 13 classes recognized as `HostedToolDef` with `SDK=google_adk`: `BashTool`, `GoogleSearchTool`, `VertexAiSearchTool`, `LangchainTool`, `CrewaiTool`, `AgentTool`, `LongRunningTool`, `LoadWebPage`, `ExitLoopTool`, `GoogleMapsGroundingTool`, `UrlContextTool`, `DiscoveryEngineSearchTool`, `EnterpriseSearchTool` |
| sub_agents edges | `sub_agents=[...]` kwargs resolved into `HandoffRefs` pointing to same-file `AgentDef`s (resolved by both the `name=` literal and the assignment-target variable) |

**Limitation:** `AgentTool` wraps another agent. The wrapped agent is recorded as a `HostedToolDef` edge but is not transitively analyzed — its tools, guardrails, and sub-agents are not walked further.

### Google ADK — TypeScript

Discovery sources: `internal/analysis/ts_adk_tools.go`, `ts_adk_agents.go`,
`ts_adk_hosted_tools.go`, plus the shared `ts_handler_facts.go`. Import gate:
only files importing from `@google/adk` are processed (handled by the
`TSImportAliasesAny` union helper). ADK JS is a single package — no
`-core` / `-openai`-style sibling packages.

| Construct | Recognition |
|---|---|
| Agents | `new LlmAgent({...})` / `new SequentialAgent({...})` / `new ParallelAgent({...})` / `new LoopAgent({...})` / `new RoutedAgent({...})` (5 classes; no `Agent` alias unlike Python ADK). All option-object kwargs captured into a typed `KwargTree`; `Opaque=true` when the arg is not an object literal or contains a `...spread`. Pre-resolves hosted-tool class instantiations inside `tools: [...]` during discovery against `TSADKHostedToolClasses`; identifier-valued refs in `tools` / `subAgents` are wired by `ResolveEdges` |
| Tools (FunctionTool) | `new FunctionTool({name, description, parameters, execute, ...})` constructor calls. Class instantiation with an options object (NOT a function-wrapper like Python's `FunctionTool(my_fn)`). Captures: `name` / `description` from string literals, `parameters` top-level keys as `ParamNames`, handler body facts via shared `tsHandlerFacts` (`shells_out`, `http_call`), and leaf option fields (`isLongRunning`, etc.) flattened into `Config`. `VarName` from the enclosing `const x = new FunctionTool({...})` binding. Reuses `KindADKFunctionTool` — the `Language` field distinguishes the JS options-object shape from Python's function-wrapper shape |
| Hosted tools | Closed set of 13 classes recognized as `HostedToolDef` with `SDK=google_adk`: `AgentTool`, `ExitLoopTool`, `GoogleMapsGroundingTool`, `GoogleSearchTool`, `LoadArtifactsTool`, `LoadMemoryTool`, `LongRunningTool`, `PreloadMemoryTool`, `UrlContextTool`, `VertexAiSearchTool`, `VertexRagRetrievalTool`, `RunSkillInlineScriptTool`, `RunSkillScriptTool`. Partial overlap with Python's 13 classes (7 shared, 6 JS-only, 6 Python-only with no JS factory) |
| subAgents edges | `subAgents: [...]` (camelCase, unlike Python's `sub_agents=`) kwargs resolved into `HandoffRefs` pointing to same-file `AgentDef`s via the language-agnostic ResolveEdges pass |

**Limitation:** `AgentTool` wraps another agent — same transitive-analysis caveat as Python ADK applies.
**v1 limitation:** only bare-identifier constructors are recognized; namespace-import constructors like `new ns.LlmAgent({...})` (a member_expression) are not handled.

### OpenShell — Python

| Construct | Recognition |
|---|---|
| Shell-invocation surfaces | Any bare function body calling `subprocess.*`, `os.system`, or `os.popen` → tagged `KindShellInvocation` in inventory; sets `RepoInventory.HasShellInvocations` (the "openshell" risk surface). Not an SDK — never appears in `SDKsDetected` |
| Sandbox policy files | `openshell/*.yaml` / `*.yml` surfaced as `sandbox_policy` components |
| Detection trigger | An `openshell/` directory, or any YAML declaring an OpenShell schema (`openshell.nvidia.com/v`). No dependency-manifest needle — OpenShell is recognized by artifact presence and shell-invocation surfaces, not by a declared dep |

The OSH-001..005 detection rules previously shipped here; they moved to a
closed-source companion project. With no OSH rules shipped, a repo with
shell-invocation surfaces fires **no** rule and **no** META finding — but
the human report still surfaces it usefully. The `Risk surfaces: openshell`
block reports the count of shell-invoking functions, the first three
file:line locations (deterministically sorted), a `why:` line stating the
threat model (a prompt-injected agent that exposes one of these as a tool
can run arbitrary commands), and a `fix:` line with concrete remediations
(sandbox, allowlist, drop `shell=True`, keep shell logic out of
agent-callable code). The renderer does NOT claim an audit happened, since
no openshell rule pack ships. Repo-scope rules with `applies_to: [openshell]`
(if any are loaded from a private pack) gate on `HasShellInvocations`.
OpenShell is deliberately not treated as an unaudited SDK, so META-001 does
not fire for it.

## Gaps and what it would take to close them

| Gap | Effort sketch |
|---|---|
| **Claude SDK TypeScript rules** (`@anthropic-ai/claude-agent-sdk`) | Discovery is done (SP1). Remaining: per-language predicate implementations in `rules/predicates.go` and a TS-language rule pack in the `trustabl-rules` repository. Currently produces META-004 (policy loaded, no rule applicable to TS inputs) |
| **MCP cross-language** (TS, Rust, Go) | Two prerequisites are missing today: (1) MCP dep-scan needles in `internal/ingestion/normalizer.go` — currently only `claude-agent-sdk` / `claude_agent_sdk` / `openai-agents` / `@openai/agents` / `google-adk` / `@google/adk` are matched; there is no `@modelcontextprotocol/sdk` (npm), no `rmcp` / `anthropic-mcp` (Cargo), no Go MCP module needle. (2) per-language AST parsers and discovery for the SDK shapes (`Server.tool()` factory in TS, `#[tool]` macros in Rust, etc.). File paths are recorded by the generic walk but no MCP-specific extraction happens against them |
| **MCP server-side completeness** | We discover tools registered with `@server.tool` etc., but don't extract `Prompt`, `Resource`, `Sampling` registrations — those exist in the spec and would be a small additional pass |

## Recommended next moves

This section is editorial — recorded here so future contributors see the
rationale, not as a binding roadmap.

1. **TypeScript parser** has now landed for Claude SDK TS, OpenAI Agents
   JS, and Google ADK JS — the same infra investment still covers the
   remaining TS targets (TS MCP servers). The discovery patterns are
   different per SDK but the AST plumbing is shared.
2. **OpenAI Agents TS rule pack** is the highest-leverage near-term move
   now that TS OpenAI discovery is wired (SP2). The Python OAI-* rules
   (OAI-001..015 tool, OAI-101..104/106/109/110/111 agent, OAI-201 repo) all have direct
   TS analogues — most are the same conceptual check, retargeted at
   `tool({...})` factory args / `new Agent({...})` option-object kwargs
   instead of Python decorator + constructor shapes — and would clear the
   META-004 finding TS OpenAI repos currently produce.
3. **Google ADK TS rule pack** is the parallel near-term move now that
   TS ADK discovery is wired (SP2). The Python ADK-* rules
   (ADK-001..007 tool, ADK-008/101..108/110 agent) have direct TS analogues —
   retargeted at `new FunctionTool({...})` option-object args /
   `new LlmAgent({...})` option-object kwargs instead of Python
   `FunctionTool(symbol)` + constructor shapes — and would clear the
   META-004 finding TS ADK repos currently produce.
4. **MCP rule pack** would be a small detection win — we already discover
   MCP tools, but no rules target them. Useful checks include "MCP tool
   without input schema" and "stdio MCP server with absolute path to a
   binary outside the repo."

## Sources

- [google/adk-python](https://github.com/google/adk-python) — Google's official Python ADK
- [google/adk-js](https://github.com/google/adk-js) — Google's official TypeScript ADK
- [Agent Development Kit docs](https://google.github.io/adk-docs/) — ADK documentation
- [Introducing ADK for TypeScript](https://developers.googleblog.com/introducing-agent-development-kit-for-typescript-build-ai-agents-with-the-power-of-a-code-first-approach/) — Google Developers blog announcement
- [openai/openai-agents-js](https://github.com/openai/openai-agents-js) — OpenAI Agents TypeScript SDK
- [OpenAI Agents SDK TypeScript docs](https://openai.github.io/openai-agents-js/)
- [anthropics/claude-agent-sdk-typescript](https://github.com/anthropics/claude-agent-sdk-typescript) — Claude Agent SDK TypeScript repo
- [@anthropic-ai/claude-agent-sdk on npm](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk)
