---
name: trustabl-scan
description: >-
  Use right after you write or modify AI agent, tool, subagent, or MCP-server
  code (OpenAI Agents SDK, Claude Agent SDK, Google ADK, MCP) to self-audit it
  for security and reliability misconfigurations with Trustabl before
  committing. Triggers on adding or editing an agent definition, a tool /
  @function_tool / @tool / tool() handler, a subagent markdown file, an MCP
  server registration, agent guardrails, or .claude/settings.json permissions.
  Runs Trustabl's `scan` tool via the plugin's bundled MCP server and guides
  remediation of the findings.
---

# Self-audit agent code with Trustabl

Trustabl is a static analyzer for agent-SDK code. It models the tools, agents,
subagents, and MCP servers a repo declares and checks them against a rule
catalog, reporting reliability and safety weaknesses with an explanation, a
suggested fix, and a confidence score. Run it as a self-audit at generation
time, upstream of CI, so issues are caught and fixed before the commit.

Trustabl is read-only: it detects and reports, and never writes to or modifies
the scanned repo. You apply the fixes yourself, then re-scan to confirm.

## When to use this

Invoke after you have just written or changed any of these, before committing:

- An agent definition (`Agent(...)`, `SandboxAgent(...)`, `AgentDefinition`,
  `new Agent({...})`, ADK `LlmAgent` / `SequentialAgent` / etc., or a TS
  `query(...)` main-thread agent).
- A tool definition (a `@function_tool` / `@tool` / `@claude_tool` function, a
  TS `tool(...)` factory call, an ADK `FunctionTool` wrapper, or a shell-invoking
  function).
- A subagent markdown file (`.claude/agents/*.md`), a skill, a slash command, or
  a plugin manifest.
- An MCP server registration (`@server.tool` / `@mcp.tool` / `createSdkMcpServer`)
  or an MCP config.
- Agent guardrails (`@input_guardrail` / `@output_guardrail`) or
  `.claude/settings.json` permission settings.

Languages understood today: Python and TypeScript (`.ts` / `.tsx` / `.mts` /
`.cts`). JavaScript and Go files are inventoried but not AST-parsed, so no tools
or agents are extracted from them.

## How to run the scan

Scanning runs through this plugin's **bundled MCP server** (`trustabl`), which
exposes a `scan` tool. Call the tool **`mcp__trustabl__scan`** with the path to
the repo you just edited:

- `mcp__trustabl__scan` with `{"path": "."}` — scan the current repo.
- Optionally `{"path": ".", "rules_ref": "<branch-or-tag>"}` to pin the
  detection-rules ref.

The tool returns the full scan result as JSON — the same `ScanResult` shape as
the CLI's `--format json` output, with a `findings` array to reason over. There
is no exit code: decide what is actionable by reading finding **severities**
(`medium` and above are commit-blockers; see "How to read findings"). The server
fetches and caches the `trustabl-rules` pack itself, falling back to its local
cache when offline.

**CLI fallback.** If the `mcp__trustabl__scan` tool is not available this session
(for example the MCP server failed to start), run the CLI directly with the
resolved binary — `"$TRUSTABL_BIN"` when that variable is set, else `trustabl`
on `PATH`:

```bash
"$TRUSTABL_BIN" scan . --format json          # JSON, same shape the tool returns
"$TRUSTABL_BIN" scan . --strict               # exit 1 on low+ findings (info/META never fail)
"$TRUSTABL_BIN" scan . --detectors claude_sdk # narrow to one SDK: claude_sdk|openai_sdk|google_adk|openshell
```

In the CLI fallback the exit code is the gate: `0` = no findings of medium
severity or higher, `1` = at least one, `2` = scanner/I-O error or no usable
rules (run `"$TRUSTABL_BIN" rules pull` once to pre-populate the rules cache).

## How to read findings

Each finding carries:

- **severity** (`info` / `low` / `medium` / `high` / `critical`) how bad it is.
  Treat `medium` and above as commit-blockers; `info` / `META` never block. (In
  the CLI fallback these map to the exit code: `medium`+ → exit 1, or `low`+
  under `--strict`.)
- **confidence** a heuristic score for how sure the rule is. It is not
  LLM-judged and not yet calibrated, so treat findings as signal to investigate,
  not ground truth. Look at higher-confidence findings first.
- **explanation** why the pattern is a problem.
- **suggested_fix** the concrete remediation to apply.
- **location** where it fired, attributed to one of four scopes:
  - **tool** a specific tool definition (file and line). Example: an HTTP call
    with no timeout, untyped parameters, a missing docstring.
  - **agent** a specific agent at its constructor call site, with its resolved
    tool / handoff / guardrail edges. Example: an agent wired with shell tools
    but no input guardrails.
  - **subagent** a `.claude/agents/*.md` declaration. Example: a subagent
    granted `Bash` despite a read-only description.
  - **repo** the whole repo, once per scan. Example: an SDK present with no
    custom trace processor configured.

`META` findings (for example "this repo uses an SDK Trustabl does not currently
audit", or "this dep is declared but unused") are honest coverage signals, not
defects, and never fail the build. Two agents in the same repo can be in very
different postures, so always fix the agent the finding names, not the repo as a
whole.

## Remediation loop

1. Call `mcp__trustabl__scan` with `{"path": "."}` and read the `findings` array.
2. For each finding of `medium` or higher, read its `explanation` and
   `suggested_fix` and apply the fix to the code (Trustabl will not edit files
   for you).
3. Call the scan tool again and confirm the finding is gone from `findings`.
4. Repeat until only `info` / `META` signals remain. For a stricter bar before
   committing, treat `low` findings as blockers too (or use the CLI fallback's
   `--strict`).

## Installing the binary

Normally you do not need to. The plugin installs the pinned CLI itself: the
bundled MCP launcher (`scripts/trustabl-mcp.sh`) and the `SessionStart` hook
(`scripts/check-trustabl.sh`) share install logic (`scripts/lib-trustabl.sh`)
that downloads the pinned version, verifies it against the release
`checksums.txt`, and installs it into the plugin's private data directory. That
one binary both backs the `mcp__trustabl__scan` server and is exposed as
`$TRUSTABL_BIN` for direct CLI use. It installs only into the plugin's own data
dir — never the user's system or their own `trustabl` — and is reversible.

Auto-install is skipped only when it cannot run: offline on the very first
session, an unsupported platform (e.g. native Windows without bash), or missing
`curl` / `tar`. It then falls back to whatever `trustabl` is on `PATH`. If you
see a `[trustabl] could not provide the trustabl CLI …` notice, or the
`mcp__trustabl__scan` tool is missing and no binary resolves, offer to install it
system-wide, **asking the user before you run any install command; never install
silently**:

```bash
# macOS / Linux (Homebrew)
brew install trustabl/tap/trustabl

# Windows (Scoop)
scoop bucket add trustabl https://github.com/trustabl/scoop-bucket
scoop install trustabl

# Docker (no local install; mount the repo at /repo)
docker run --rm -v "$PWD:/repo" ghcr.io/trustabl/trustabl:latest scan /repo
```

Prebuilt archives for each platform are on the GitHub Releases page
(https://github.com/trustabl/trustabl/releases). Confirm the install with
`trustabl version`.
