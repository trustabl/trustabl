# Coverage

Coverage matrix for Trustabl's static analysis: which agent SDKs (and which
languages) we currently scan, analyse, and detect against. This file is the
at-a-glance reference; `ARCHITECTURE.md` has the implementation detail.

_Last reviewed: 2026-06-04 (LangChain / LangGraph SDK row added)._

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
| **Claude Agent SDK** | Python | ✅ dep-scan + file inventory + `.claude/` & `.claude-plugin/` components | ✅ tools, agents, subagents (canonical + flat-collection shape fallback), skills (`SKILL.md`), slash commands, plugin manifests, settings, `ClaudeAgentOptions` session config | ✅ tool CSDK-001..009, 107, 108 (008 = `**kwargs` without input_schema, 009 = SSRF); agent CSDK-101..105; subagent CSDK-110, 111 (fire on pure-markdown collections); repo CSDK-201..203 (`defaultMode` / `permission_mode` bypass; 203 = SDK code without an agent-guidance doc: AGENTS.md/CLAUDE.md) |
| **Claude Agent SDK** | TypeScript | ✅ dep-scan (`@anthropic-ai/claude-agent-sdk`) + file inventory + `.claude/` components | ✅ tools (`tool()` factory), agents (main thread `QueryMainAgent` per `query()` call + sub-agents inline-in-query + typed-const `AgentDefinition`), MCP servers (createSdkMcpServer + 4 config literals) | ◐ tool CSDK-010 (shell), 011 (eval/new Function), 012 (fs-write), 013 (SSRF / dynamic URL), 014 (no description), 016 (mutating tool no idempotency key); agent CSDK-120 (permissionMode bypass), 130 (`query()` main agent grants Bash), 131 (`query()` main agent grants write/fetch built-ins); META-004 no longer fires |
| **OpenAI Agents SDK** | Python | ✅ dep-scan + file inventory | ✅ tools, hosted tools (11 classes), agents, MCP servers (3 transports + alias), guardrails, sessions | ✅ tool OAI-001..015, 018 (018 = SSRF / caller-controlled URL); agent OAI-101..104, 106, 109, 110, 111; repo OAI-201, 202 (202 = SDK code without an agent-guidance doc: AGENTS.md/CLAUDE.md) |
| **OpenAI Agents SDK** | TypeScript | ✅ dep-scan (`@openai/agents` substring catches `-core` / `-openai`) + file inventory | ✅ tools (`tool({...})` factory), agents (`new Agent({...})` + `Agent.create(...)`), hosted tools (9 factories across `@openai/agents-core` and `@openai/agents-openai`), MCP servers (3 transports + `MCPServers` wrapper), guardrails (4 `defineX` factories), sessions (`MemorySession` / `OpenAIConversationsSession` / `OpenAIResponsesCompactionSession`) | ✅ tool OAI-016 (fetch without AbortSignal timeout), OAI-017 (eval / new Function), OAI-019 (mutating tool without idempotency), OAI-022 (no description), OAI-024 (dynamic URL / SSRF); agent OAI-105 (content hosted-tool without inputGuardrails); all with fire/silent cases in the per-rule harness |
| **MCP** | Python | ✅ tool registrations + config files | ◐ tool registrations only (no server-side resource/prompt discovery) | ✅ dedicated `mcp/` pack: tool MCP-001..010 (001 no description, 002 untyped params, 003 ambiguous name, 004 network timeout, 005 path safety, 006 error contract, 007 idempotency, 008 SSRF, 009 code-exec, 010 shell). `mcp_tool` coverage now lives ONLY in this pack — stripped from the CSDK rules' `applies_to` so a pure-MCP repo is covered and a mixed Claude+MCP repo does not double-fire |
| **MCP** | TypeScript | ✅ dep-scan (`@modelcontextprotocol/sdk`) + file inventory | ✅ server authoring: `new McpServer(...)` receiver tracked, `registerTool` / legacy `tool` registrations → `KindMCPTool` (reuses `tsZodParamNames` / `tsHandlerFacts`). Distinct from the Claude client-config `createSdkMcpServer` discovery. Low-level `Server` + `setRequestHandler` not extracted (gap) | ✅ tool MCP-011..014 (011 no description, 012 shell, 013 SSRF, 014 eval / new Function); shared `mcp/` pack, `language: typescript` |
| **MCP** | Go / Rust | ❌ no MCP-specific recognition (no Go/Rust AST parser, no dep needles) | ❌ | ❌ |
| **Google ADK** | Python | ✅ dep-scan (`google-adk`) + file inventory | ✅ LlmAgent (+ Agent alias), SequentialAgent, ParallelAgent, LoopAgent, LanggraphAgent; FunctionTool wrapping; 13 built-in hosted tools; sub_agents edges | ✅ tool ADK-001..007, 009..012 (009 = print to stdout, 010 = subprocess, 011 = eval/exec/compile, 012 = SSRF); agent ADK-008, 101..108, 110; repo ADK-201 (SDK code without an agent-guidance doc: AGENTS.md/CLAUDE.md) |
| **Google ADK** | TypeScript | ✅ dep-scan (`@google/adk`) + file inventory | ✅ tools (`new FunctionTool({...})`), agents (5 constructors: LlmAgent + SequentialAgent + ParallelAgent + LoopAgent + RoutedAgent), hosted tools (13 classes), subAgents edges | ◐ tool ADK-013 (no description), 015 (eval / new Function), 016 (SSRF / dynamic URL); agent ADK-109 (LlmAgent no description); first ADK TS pack; META-004 no longer fires |
| **Google ADK** | Go / Java / Kotlin | ❌ | ❌ | ❌ |
| **LangChain / LangGraph** | Python | ✅ dep-scan (`langchain` / `langgraph` needles, all manifests) + file inventory | ✅ tools (`@tool` decorator — import-gated to disambiguate from the Claude SDK's `@tool`; `StructuredTool` / `Tool` factories + `.from_function`), agents (`create_react_agent`, `create_agent`, `AgentExecutor`; positional `tools` captured), dangerous built-ins (`PythonREPLTool` / `PythonAstREPLTool` / `ShellTool` / `Requests*` → `HostedToolDef` edges) | ✅ tool LC-001 (no description), LC-002 (untyped params), LC-003 (shell), LC-004 (code-exec), LC-005 (SSRF), LC-006 (`return_direct`); agent LC-101 (code-exec/shell built-in), LC-102 (AgentExecutor no `max_iterations`); repo LC-201 (no agent-guidance doc) |
| **LangChain / LangGraph** | TypeScript | ✅ dep-scan (`@langchain/*` / `langchain` / `langgraph`) + file inventory | ✅ tools (`tool(fn, {...})` factory — import-gated, config from arg 1; `DynamicStructuredTool` / `DynamicTool`), agents (`createReactAgent`, `createAgent`, `new AgentExecutor`) | ◐ tool LC-010 (no description), LC-011 (shell), LC-012 (code-exec), LC-013 (SSRF), LC-014 (`returnDirect`); agent LC-111 (AgentExecutor no `maxIterations`). Provider hosted tools (`shell()` / `bash_*` / `applyPatch`) and the raw `StateGraph` graph agent are documented gaps |
| **OpenShell** | Python | ✅ shell-invocation discovery + `openshell/*.yaml` policy files surfaced | ✅ `KindShellInvocation` tools → `RepoInventory.HasShellInvocations` (the "openshell" risk surface; not an SDK, never in `SDKsDetected`) | ❌ rules moved to closed-source companion project (no rule fires; no META finding — openshell is not treated as an unaudited SDK) |

## What we parse exactly (per SDK)

### Claude Agent SDK — Python

Discovery sources: `internal/analysis/discovery.go`, `agents.go`, `subagents.go`,
`claude_settings.go`.

| Construct | Recognition |
|---|---|
| Tools | Decorators matched by resolved callee path: `@tool`, `@claude_tool`, `@agent.tool`, or a decorator whose callee contains `claude_agent_sdk`. Exact-callee matching (not substring), so unrelated decorators like `@toolbar` / `@tool_registry.register` no longer misclassify as tools. Captures: function name, params, type annotations, docstring, decorator kwargs |
| Agents | `AgentDefinition(...)` constructor calls. Captures every kwarg into a typed `KwargTree`: `name`, `description`, `prompt`, `tools`, `disallowedTools`, `permissionMode`, `mcpServers`, `skills`, `memory`, `maxTurns`, `background`, `effort`, `initialPrompt`. Typed accessors expose `tools`/`disallowedTools`/`permissionMode`/`mcpServers` without reaching into the tree |
| Subagents | **Hybrid** discovery: canonical `.claude/agents/*.md` (any path depth, monorepo-safe) PLUS a frontmatter-shape fallback over all markdown files (gate: `name` + `tools`/`model`, excluding `SKILL.md` and `.claude/commands/`) that catches flat collections like `VoltAgent/awesome-claude-code-subagents` (subagents under `categories/<NN>/*.md`). Captures `name`, `description`, `tools` (verbatim) + `ToolGrants` (parsed grammar: bare / `Bash(...)` / `mcp__server__tool`), `disallowedTools`, `model`, `permissionMode`, `mcpServers`, `skills`, `isolation`, `HasHooks`. Files without frontmatter or without a `name:` are skipped. Audited by subagent-scope rules — CSDK-110 ("Subagent granted the built-in Bash tool") and CSDK-111 ("Subagent granted filesystem-write or web-fetch built-ins") are the shipped rules; predicate `subagent_grants_tool: [Bash]` matches parsed grants (so `Bash(...)` matches `Bash`). Subagent presence alone marks the repo as `claude_agent_sdk` (no SDK code required), so the pack loads and CSDK-110/111 fire on pure-markdown collections. Subagent rules carry no `language:` field (markdown frontmatter, language-agnostic) |
| Skills | `SKILL.md` (basename, any depth: `.claude/skills/<name>/SKILL.md`, plugin `skills/`, nested). Parses `name`, `description`, `allowed-tools` (space-separated or YAML-list → `ToolGrants`), `argument-hint`, `disable-model-invocation`. No rules target skills yet — surfaced in inventory/JSON |
| Slash commands | **Two path shapes**: canonical `.claude/commands/*.md` AND `<plugin-root>/commands/*.md` whenever `<plugin-root>` has a sibling `.claude-plugin/plugin.json` (the layout used by plugin-distribution repos like `wshobson/agents` — `plugins/<x>/commands/*.md`). Command name = file basename. Parses frontmatter `description`, `allowed-tools`, `model`, `argument-hint`, `disable-model-invocation`; a command without frontmatter is still emitted (body is the prompt). No rules yet — surfaced in inventory/JSON |
| Plugin manifests | `.claude-plugin/plugin.json` and `marketplace.json` JSON-parsed into `PluginManifest` (`kind` plugin/marketplace, `name`, catalog `plugins[]` with `name` + normalized `source`). The `source` field accepts both forms in the wild: a plain string (`"./local-foo"`) or an object (`{"source":"git-subdir","url":"…","path":"…"}` for external git refs); object forms are normalized to `<source>:<url>#<path>` so the trust category survives the round-trip, unknown shapes fall back to raw JSON. A previous typed-string parser dropped the entire manifest on the object form. The recon walk descends into `.claude-plugin/`. No rules yet — surfaced in inventory/JSON |
| Settings | `.claude/settings.json` and `settings.local.json` JSON-parsed: `permissions.allow`/`deny`/`ask` decomposed via the grammar `<Tool>` \| `<Tool>(<pattern>)` plus `mcp__<server>__<tool>`; `defaultMode`, `additionalDirectories`, presence flags for `env`/`hooks`/`sandbox` |
| Session config | `ClaudeAgentOptions(...)` constructor calls. Captures kwargs into a `KwargTree`; `permission_mode` is read by the repo-scope rule CSDK-202. Presence alone marks the repo `claude_agent_sdk` so the pack loads for options-only repos |
| Components surfaced (path-only) | `CLAUDE.md`, `AGENTS.md`, `hooks/*.{py,ts,js,jsx,mjs}`, MCP configs (`mcp.json`, `mcp_servers.json`, `claude_desktop_config.json`) |

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
| Hosted tools | Closed set of 11 classes inside `tools=[...]`: `WebSearchTool`, `FileSearchTool`, `ComputerTool`, `HostedMCPTool`, `CodeInterpreterTool`, `ImageGenerationTool`, `LocalShellTool`, `ShellTool`, `ApplyPatchTool`, `CustomTool`, `ToolSearchTool` → emits `HostedToolDef` + `HostedToolRef` edge. Matched bare or module-qualified (`agents.WebSearchTool()`) |
| MCP servers | Closed set of 3 classes inside `mcp_servers=[...]`: `MCPServerStdio` (stdio), `MCPServerSse` (sse), `MCPServerStreamableHttp` (streamable_http), matched bare or module-qualified (`mcp.MCPServerStdio()`). Both inline construction AND `async with X() as srv:` alias resolution (single-file scope) |
| handoffs edges | `handoffs=[...]` kwargs resolved into `HandoffRefs` pointing to same-file `AgentDef`s (resolved by both the `name=` literal and the assignment-target variable). A list item wrapped in the `handoff(...)` helper resolves to `external` |
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
| Tools | `tool({name, description, parameters, execute, ...})` factory calls. Captures: `name` / `description`, `parameters` keys as `ParamNames` — both an inline object literal (`{ city: ... }`) and a Zod schema constructor (`z.object({...})`, with chained refinements like `.strict()` unwrapped); any schema constructor sets `HasTypedParams` even when keys cannot be enumerated. Plus handler body facts via shared `tsHandlerFacts` (`shells_out`, `http_call`), and option fields (`strict`, `needsApproval`, `timeoutMs`, etc.) flattened into `Config`, including nested objects with dot-joined keys (e.g. `annotations.readOnlyHint`). `VarName` from the enclosing `const x = tool({...})` binding |
| Agents | `new Agent({...})` and `Agent.create({...})`. All option-object kwargs captured into a typed `KwargTree`; `Opaque=true` when the arg is not an object literal or contains a `...spread`. Pre-resolves hosted-tool factory calls inside `tools: [...]` during discovery; identifier-valued refs in `tools`/`handoffs`/`inputGuardrails`/`outputGuardrails`/`mcpServers` are wired by `ResolveEdges` via a Name+VarName double-indexed lookup. An inline `tool({...})` (or any unrecognized call/`new` in `tools`) marks the agent `Opaque` so "no tools" rules don't false-fire |
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
Import gate (AST-based): only files that actually import `google.adk` via an
`import` / `from … import` statement are processed — a comment or string literal
that merely mentions the module does not trip the gate — which prevents the bare
`Agent` class name from colliding with OpenAI's identically-named class.

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
| Agents | `new LlmAgent({...})` / `new SequentialAgent({...})` / `new ParallelAgent({...})` / `new LoopAgent({...})` / `new RoutedAgent({...})` (5 classes; no `Agent` alias unlike Python ADK). All option-object kwargs captured into a typed `KwargTree`; `Opaque=true` when the arg is not an object literal or contains a `...spread`. Pre-resolves hosted-tool class instantiations inside `tools: [...]` during discovery against `TSADKHostedToolClasses`; identifier-valued refs in `tools` / `subAgents` are wired by `ResolveEdges`. An inline `new FunctionTool({...})` (or any unrecognized call/`new` in `tools`) marks the agent `Opaque` |
| Tools (FunctionTool) | `new FunctionTool({name, description, parameters, execute, ...})` constructor calls. Class instantiation with an options object (NOT a function-wrapper like Python's `FunctionTool(my_fn)`). Captures: `name` / `description` from string literals, `parameters` keys as `ParamNames` — both an inline object literal and a Zod schema constructor (`z.object({...})`, chained refinements unwrapped; any schema constructor sets `HasTypedParams`), handler body facts via shared `tsHandlerFacts` (`shells_out`, `http_call`), and option fields (`isLongRunning`, etc.) flattened into `Config`, including nested objects with dot-joined keys. `VarName` from the enclosing `const x = new FunctionTool({...})` binding. Reuses `KindADKFunctionTool` — the `Language` field distinguishes the JS options-object shape from Python's function-wrapper shape |
| Hosted tools | Closed set of 13 classes recognized as `HostedToolDef` with `SDK=google_adk`: `AgentTool`, `ExitLoopTool`, `GoogleMapsGroundingTool`, `GoogleSearchTool`, `LoadArtifactsTool`, `LoadMemoryTool`, `LongRunningTool`, `PreloadMemoryTool`, `UrlContextTool`, `VertexAiSearchTool`, `VertexRagRetrievalTool`, `RunSkillInlineScriptTool`, `RunSkillScriptTool`. Partial overlap with Python's 13 classes (7 shared, 6 JS-only, 6 Python-only with no JS factory) |
| subAgents edges | `subAgents: [...]` (camelCase, unlike Python's `sub_agents=`) kwargs resolved into `HandoffRefs` pointing to same-file `AgentDef`s via the language-agnostic ResolveEdges pass |

**Limitation:** `AgentTool` wraps another agent — same transitive-analysis caveat as Python ADK applies.
**v1 limitation:** only bare-identifier constructors are recognized; namespace-import constructors like `new ns.LlmAgent({...})` (a member_expression) are not handled.

### LangChain / LangGraph — Python

Discovery sources: `internal/analysis/discovery.go` (the `@tool` decorator,
import-routed), `langchain_tools.go`, `langchain_agents.go`,
`langchain_hosted_tools.go`. Import gate (AST-based): a file is in scope when it
imports the langchain ecosystem (`langchain`, `langchain_core`, `langgraph`,
`langchain_community`, `langchain_experimental`, `langchain_classic`, the
`langchain-*` provider packages).

| Construct | Recognition |
|---|---|
| Tools (`@tool`) | The `@tool` decorator (shared with the Claude SDK), classified in `kindFromDecorators` by the **import binding** of the `tool` symbol (`collectToolImports`): `tool` bound from a langchain module → `KindLangChainTool`, from `claude_agent_sdk` → Claude, last-binding-wins on shadowing — so it attributes correctly even when a file imports both SDKs. A file-level import-presence check is the fallback for a star-import / locally defined `tool`. Captures name, docstring → Description, typed-params, decorator kwargs (incl. `return_direct`) → Config |
| Tools (factories) | `StructuredTool(...)` / `StructuredTool.from_function(fn)` / `Tool(...)` / `Tool.from_function(fn)`. Resolves the wrapped function (first positional arg, or `func=`) to a same-file def and points the ToolDef at its body so the shell/code/SSRF predicates scan the implementation; explicit `name=` / `description=` / `args_schema=` override. `class X(BaseTool)` is a documented gap |
| Agents | `create_react_agent(...)` / `create_agent(...)` / `AgentExecutor(...)` → `AgentDef` with normalized Class `ReactAgent` / `CreateAgent` / `AgentExecutor`. All kwargs captured; the positional `tools` argument (index 1) of the two factories is captured as a synthetic `tools` kwarg so edge + hosted-tool resolution sees it |
| Dangerous built-ins | `PythonREPLTool`, `PythonAstREPLTool`, `ShellTool`, and the `Requests*` family inside an agent's resolved `tools` list → `HostedToolDef` (SDK `langchain`), consumed by agent rule LC-101 |

**Limitation:** the raw `StateGraph` → `.add_node` → `.compile()` graph agent is
not modeled (it is emergent across many call sites). Tool-edge / hosted-tool
resolution requires `tools` to be a kwarg or the captured positional; a
`ToolNode`-indirected or fully dynamic tool list is left unresolved.

### LangChain / LangGraph — TypeScript

Discovery sources: `internal/analysis/ts_langchain_tools.go`,
`ts_langchain_agents.go`, plus the shared `ts_handler_facts.go`. Import gate:
prefix-matched (`astutil.TSImportAliasesMatch`) so the many subpaths
(`@langchain/core/tools`, `@langchain/langgraph/prebuilt`, `langchain`,
`langgraph`, any `@langchain/*`) are all in scope.

| Construct | Recognition |
|---|---|
| Tools | `tool(fn, { name, description, schema })` (config is arg 1, unlike OpenAI's arg-0 — the import gate keeps the shared `tool()` name from cross-firing with the Claude/OpenAI passes), `new DynamicStructuredTool({...})`, `new DynamicTool({...})`. Name/description/`schema` (Zod or literal) → typed-params; handler body facts (`shells_out`, `code_exec`, `dynamic_url`) via `tsHandlerFacts`; `returnDirect` → Config |
| Agents | `createReactAgent({...})` / `createAgent({...})` / `new AgentExecutor({...})` → `AgentDef` with normalized Class `ReactAgent` / `CreateAgent` / `AgentExecutor`. Kwargs captured; identifier `tools` refs wired as ToolRefs; an inline or `ToolNode` tool list marks the agent `Opaque` |

**Limitation:** provider-package hosted tools (`@langchain/openai` `shell()` /
`localShell()`, `@langchain/anthropic` `bash_*` — date-stamped names) and
class-based tools (`extends StructuredTool`) are documented gaps; the raw
`StateGraph` graph agent is not modeled.

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
| **Claude SDK TypeScript rules** (`@anthropic-ai/claude-agent-sdk`) | Shipped: tool CSDK-010 (shell), 011 (eval/new Function), 012 (fs-write), 013 (SSRF/dynamic URL), 014 (no description), 016 (mutating tool no idempotency key); agent CSDK-120 (permissionMode bypass), 130/131 (`query()` main-thread agent grants Bash / write-fetch built-ins). The TS predicate machinery is in place (structural `shells_out`/`writes_fs`/`dynamic_url`/`code_exec` facts read by `has_shell_call`/`has_write_call`/`has_dynamic_url_call`/`has_code_exec_call` language branches, plus a `has_body_text` line-span substring fallback kept only for inherently *textual-absence* checks) and covered by the per-rule fire/silent harness. CSDK-010 (shell) and CSDK-012 (fs-write) now match structurally, not by substring. Remaining breadth-parity gaps vs the Python CSDK set: typed-params (no viable TS predicate today, see note), network timeout and idempotency (intentionally still `has_body_text` — they test for the *absence* of a textual marker, which has no call-shape), error-handling. The `query()` main-thread agent surface (`claude_query_main`) is now audited by 130/131, which nothing previously checked |
| **MCP cross-language** (TS, Rust, Go) | Two prerequisites are missing today: (1) MCP dep-scan needles in `internal/ingestion/normalizer.go` — currently only `claude-agent-sdk` / `claude_agent_sdk` / `openai-agents` / `@openai/agents` / `google-adk` / `@google/adk` are matched; there is no `@modelcontextprotocol/sdk` (npm), no `rmcp` / `anthropic-mcp` (Cargo), no Go MCP module needle. (2) per-language AST parsers and discovery for the SDK shapes (`Server.tool()` factory in TS, `#[tool]` macros in Rust, etc.). File paths are recorded by the generic walk but no MCP-specific extraction happens against them |
| **MCP server-side completeness** | We discover tools registered with `@server.tool` etc., but don't extract `Prompt`, `Resource`, `Sampling` registrations — those exist in the spec and would be a small additional pass |
| **LangChain class tools + raw `StateGraph` + TS provider hosted tools** | Three additive discovery gaps in the LangChain pack: `class X(BaseTool)` / `extends StructuredTool` (a class shape, not a call); the raw `StateGraph` → `.compile()` graph agent (emergent across call sites — needs data-flow from the `StateGraph` var through `add_node` / `ToolNode` / `compile`, not a single-call capture); and the date-stamped TS provider hosted tools (`shell()` / `bash_*` / `applyPatch`). Each is discovery-only, no schema change |

## Recommended next moves

This section is editorial — recorded here so future contributors see the
rationale, not as a binding roadmap.

1. **TypeScript parser** now backs every SDK Trustabl recognizes — Claude SDK
   TS, OpenAI Agents JS, Google ADK JS, MCP-proper servers (`ts_mcp_proper.go`),
   and LangChain / LangGraph TS. The discovery patterns differ per SDK but the
   AST plumbing is shared; the LangChain pass added a reusable prefix-matched
   import gate (`astutil.TSImportAliasesMatch`) for ecosystems whose imports use
   many subpaths. The remaining TS work is per-SDK *rule* parity (points 2, 3,
   and 5), not parser infrastructure.
2. **OpenAI Agents TS rule pack** has **expanded**: OAI-016 (fetch without
   AbortSignal timeout), OAI-017 (eval / new Function), OAI-019 (mutating tool
   without idempotency), OAI-022 (no description), OAI-024 (dynamic URL / SSRF),
   and agent OAI-105 (content hosted-tool without inputGuardrails), all
   `language: typescript`. TS OpenAI repos no longer produce META-004. The TS
   fire/silent harness (`parseTSTool` / `parseTSAgentInline` /
   `parseTSOpenAIAgentInline`) covers them and `TestPolicyRules_AllRulesCovered`
   enforces a case for every rule including TS. Remaining: TS analogues of the
   Python OAI path-safety and decorator-config rules (some need new TS facts).
3. **Google ADK TS rule pack** has **landed** (first ADK TS rules): tool
   ADK-013 (no description), 015 (eval / new Function), 016 (SSRF / dynamic URL),
   and agent ADK-109 (LlmAgent no description), retargeted at
   `new FunctionTool({...})` / `new LlmAgent({...})` option-object shapes. TS ADK
   repos no longer produce META-004. `has_shell_call` and `has_write_call` now
   read the `shells_out` / `writes_fs` facts for TS (so a TS ADK shell-out or
   fs-write rule is now expressible structurally). Remaining ADK parity gaps
   (typed-params, callback-gated agent rules) still need new TS predicates: ADK
   JS agent callback kwarg names need verification before porting
   ADK-102/105..107.
4. **MCP rule pack** has **landed** — the dedicated `mcp/` pack now ships 14
   rules: tool MCP-001..010 (Python: no description, untyped params, ambiguous
   name, network timeout, path safety, error contract, idempotency, SSRF,
   code-exec, shell) and MCP-011..014 (TypeScript: no description, shell, SSRF,
   eval / new Function). `mcp_tool` coverage now lives only in this pack. Still
   open: server-side completeness (Prompt/Resource/Sampling registrations) and
   MCP discovery for Rust/Go (see the gaps table above).
5. **LangChain / LangGraph pack** has **landed** (newest SDK row): 15 rules
   (LC-001..201) across Python and TypeScript — tool no-description /
   untyped-params / shell / code-exec / SSRF / `return_direct`, agent
   code-exec-or-shell built-in + `AgentExecutor` iteration-limit, and the repo
   missing-guidance-doc rule. The `@tool` decorator (shared with the Claude SDK)
   is disambiguated by the import binding of the `tool` symbol
   (`collectToolImports`), so it attributes correctly even in a mixed-import
   file. Remaining (tracked in the gaps table): the raw `StateGraph` →
   `.compile()` graph agent (emergent across many call sites — needs data-flow
   discovery, not a single-call capture), class-based tools (`class X(BaseTool)`
   / `extends StructuredTool`), and the date-stamped TS provider hosted tools
   (`shell()` / `bash_*` / `applyPatch`).

## Sources

- [google/adk-python](https://github.com/google/adk-python) — Google's official Python ADK
- [google/adk-js](https://github.com/google/adk-js) — Google's official TypeScript ADK
- [Agent Development Kit docs](https://google.github.io/adk-docs/) — ADK documentation
- [Introducing ADK for TypeScript](https://developers.googleblog.com/introducing-agent-development-kit-for-typescript-build-ai-agents-with-the-power-of-a-code-first-approach/) — Google Developers blog announcement
- [openai/openai-agents-js](https://github.com/openai/openai-agents-js) — OpenAI Agents TypeScript SDK
- [OpenAI Agents SDK TypeScript docs](https://openai.github.io/openai-agents-js/)
- [anthropics/claude-agent-sdk-typescript](https://github.com/anthropics/claude-agent-sdk-typescript) — Claude Agent SDK TypeScript repo
- [@anthropic-ai/claude-agent-sdk on npm](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk)
