# Coverage

Coverage matrix for Trustabl's static analysis: which agent SDKs (and which
languages) we currently scan, analyse, and detect against. This file is the
at-a-glance reference; `ARCHITECTURE.md` has the implementation detail.

_Last reviewed: 2026-05-22 (HEAD `b2abf8b`)._

## Coverage matrix

Legend: ✅ full · ◐ partial · ❌ none · — N/A

| SDK | Language | Scanning | Analysis (AST discovery) | Detection rules |
|---|---|---|---|---|
| **Claude Agent SDK** | Python | ✅ dep-scan + file inventory + `.claude/` components | ✅ tools, agents, subagents, settings | ✅ CSDK-001..007 (tool), CSDK-101 (agent) |
| **Claude Agent SDK** | TypeScript | ◐ file inventory + `.claude/` components | ❌ no TS AST parser | ❌ |
| **OpenAI Agents SDK** | Python | ✅ dep-scan + file inventory | ✅ tools, hosted tools (11 classes), agents, MCP servers (3 transports + alias), guardrails, sessions | ✅ OAI-001..006 (tool), OAI-101..105 (agent), OAI-201 (repo) |
| **OpenAI Agents SDK** | TypeScript | ◐ file inventory only | ❌ no TS AST parser | ❌ |
| **MCP** | Python | ✅ tool registrations + config files | ◐ tool registrations only (no server-side resource/prompt discovery) | ❌ no dedicated pack (KindMCPTool is reachable by some CSDK rules' `applies_to`) |
| **MCP** | TypeScript / Go / Rust | ❌ no MCP-specific recognition (file paths inventoried generically, no MCP parser or dep needles) | ❌ | ❌ |
| **Google ADK** | Python | ❌ no dep needle, no AST | ❌ | ❌ |
| **Google ADK** | TypeScript / Go / Java / Kotlin | ❌ | ❌ | ❌ |
| **OpenShell** | Python | ✅ shell-invocation discovery + `openshell/*.yaml` policy files surfaced | ✅ `KindShellInvocation` tools | ❌ rules moved to closed-source companion project (META-001 fires instead) |

## What we parse exactly (per SDK)

### Claude Agent SDK — Python

Discovery sources: `internal/analysis/discovery.go`, `agents.go`, `subagents.go`,
`claude_settings.go`.

| Construct | Recognition |
|---|---|
| Tools | Decorators: `@tool`, `@claude_tool`, `@agent.tool`, any decorator containing the substring `claude_agent_sdk`. Captures: function name, params, type annotations, docstring, decorator kwargs |
| Agents | `AgentDefinition(...)` constructor calls. Captures every kwarg into a typed `KwargTree`: `name`, `description`, `prompt`, `tools`, `disallowedTools`, `permissionMode`, `mcpServers`, `skills`, `memory`, `maxTurns`, `background`, `effort`, `initialPrompt`. Typed accessors expose `tools`/`disallowedTools`/`permissionMode`/`mcpServers` without reaching into the tree |
| Subagents | `.claude/agents/*.md` frontmatter (YAML between leading `---` markers): `name`, `description`, `tools` (scalar or YAML-list form), `model`. Files without frontmatter or without a `name:` are skipped |
| Settings | `.claude/settings.json` and `settings.local.json` JSON-parsed: `permissions.allow`/`deny`/`ask` decomposed via the grammar `<Tool>` \| `<Tool>(<pattern>)` plus `mcp__<server>__<tool>`; `defaultMode`, `additionalDirectories`, presence flags for `env`/`hooks`/`sandbox` |
| Components surfaced (path-only) | `CLAUDE.md`, `.claude/commands/*.md`, `hooks/*.{py,ts,js,jsx,mjs}`, MCP configs (`mcp.json`, `mcp_servers.json`, `claude_desktop_config.json`) |

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

### MCP — Python

| Construct | Recognition |
|---|---|
| Server tool registrations | Decorators: `@server.tool`, `@mcp.tool`, and `.register_tool(...)` calls. Tagged as `KindMCPTool` in inventory |
| Config files | `mcp.json`, `mcp_servers.json`, `claude_desktop_config.json` surfaced as `mcp_config` components (paths only — not deep-parsed) |

### OpenShell — Python

| Construct | Recognition |
|---|---|
| Shell-invocation surfaces | Any bare function body calling `subprocess.*`, `os.system`, or `os.popen` → tagged `KindShellInvocation` in inventory |
| Sandbox policy files | `openshell/*.yaml` / `*.yml` surfaced as `sandbox_policy` components |
| Detection trigger | An `openshell/` directory, or any YAML declaring an OpenShell schema (`openshell.nvidia.com/v`). No dependency-manifest needle — OpenShell is recognized by artifact presence and shell-invocation surfaces, not by a declared dep |

The OSH-001..005 detection rules previously shipped here; they moved to a
closed-source companion project. Repos that use OpenShell now produce a
META-001 info finding ("Trustabl does not currently audit this SDK")
instead of firing the OSH rules.

## Gaps and what it would take to close them

| Gap | Effort sketch |
|---|---|
| **Claude SDK TypeScript** (`@anthropic-ai/claude-agent-sdk`) | Tree-sitter TS binding in `astutil/`, new discovery file mirroring Python: `query()`, `ClaudeSDKClient`, hook factories. Per-language predicate impls in `rules/predicates.go`. New TS-language rule pack |
| **OpenAI Agents SDK TypeScript** (`@openai/agents`) | Same as above — TS parser + discovery for `Agent`/`tool()` factory shape. The npm package uses a different shape than Python (e.g. `tool({})` factory rather than `@function_tool` decorator) |
| **Google ADK Python** ([`google/adk-python`](https://github.com/google/adk-python)) | New dep needle (`google-adk`), discovery for the ADK shapes — `LlmAgent`, `SequentialAgent`, `ParallelAgent`, `BaseTool`, `FunctionTool`, Workflow Runtime nodes. New SDK constant `SDKGoogleADK`, new component kind for ADK config |
| **Google ADK TypeScript** ([`google/adk-js`](https://github.com/google/adk-js)) | Depends on TS parser landing for any TS work; then ADK-JS-specific shape discovery |
| **MCP cross-language** (TS, Rust, Go) | Two prerequisites are missing today: (1) MCP dep-scan needles in `internal/ingestion/normalizer.go` — currently only `claude-agent-sdk` / `claude_agent_sdk` / `openai-agents` / `@openai/agents` are matched; there is no `@modelcontextprotocol/sdk` (npm), no `rmcp` / `anthropic-mcp` (Cargo), no Go MCP module needle. (2) per-language AST parsers and discovery for the SDK shapes (`Server.tool()` factory in TS, `#[tool]` macros in Rust, etc.). File paths are recorded by the generic walk but no MCP-specific extraction happens against them |
| **MCP server-side completeness** | We discover tools registered with `@server.tool` etc., but don't extract `Prompt`, `Resource`, `Sampling` registrations — those exist in the spec and would be a small additional pass |

## Recommended next moves

This section is editorial — recorded here so future contributors see the
rationale, not as a binding roadmap.

1. **TypeScript parser** is the single biggest unlock. One infra investment
   covers Claude SDK TS, OpenAI Agents JS, Google ADK JS, and TS MCP servers.
   The discovery patterns are different per SDK but the AST plumbing is
   shared.
2. **Google ADK Python** is the smallest standalone addition (one SDK, one
   language, parser already in place). Its shape is similar to OpenAI's
   `Agent(...)` constructor pattern, so the existing `KwargTree` machinery
   transfers.
3. **MCP rule pack** would be a small detection win — we already discover
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
