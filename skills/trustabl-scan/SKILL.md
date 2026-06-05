---
name: trustabl-scan
description: >-
  Use right after you write or modify AI agent, tool, subagent, or MCP-server
  code (OpenAI Agents SDK, Claude Agent SDK, Google ADK, MCP) to self-audit it
  for security and reliability misconfigurations with Trustabl before
  committing. Triggers on adding or editing an agent definition, a tool /
  @function_tool / @tool / tool() handler, a subagent markdown file, an MCP
  server registration, agent guardrails, or .claude/settings.json permissions.
  Runs the local `trustabl scan` CLI and guides remediation of the findings.
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

From the repo you just edited (or pass its path), run:

```bash
trustabl scan .
```

That prints a human-readable summary to stdout and live progress to stderr. The
process exit code is the gate:

- `0` no findings of medium severity or higher (clean enough to commit).
- `1` at least one finding of medium severity or higher (fix before committing).
- `2` scanner or I/O error, or no usable rules available.

Useful flags (all verified against `trustabl scan --help`):

```bash
# Machine-readable output to reason over programmatically
trustabl scan . --format json
trustabl scan . --format sarif > trustabl.sarif

# Tighten the gate: exit 1 on any finding of low severity or higher.
# info / META signals never fail the build, even under --strict.
trustabl scan . --strict

# Narrow to the SDK you just touched (faster, focused):
#   claude_sdk | openai_sdk | google_adk | openshell
trustabl scan . --detectors claude_sdk

# Disable progress animation / color (useful in captured logs)
trustabl scan . --no-progress --no-color
```

When you want findings you can parse and act on precisely, prefer
`--format json` and read the `findings` array; the human format is for the
developer reading along.

Rules are not bundled in the binary. They are fetched from the public
`trustabl-rules` repo on first scan and cached locally, then refreshed on later
scans (falling back to the cache when offline). If a scan exits `2` saying no
usable rules were found, pre-populate the cache once:

```bash
trustabl rules pull
```

## How to read findings

Each finding carries:

- **severity** (`info` / `low` / `medium` / `high` / `critical`) how bad it is.
  Only `medium` and above drive the non-zero exit code (and `low` only under
  `--strict`).
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

1. Run `trustabl scan .` (add `--format json` when you want to act on findings
   precisely).
2. For each finding of `medium` or higher, read its `explanation` and
   `suggested_fix` and apply the fix to the code (Trustabl will not edit files
   for you).
3. Re-run the same scan and confirm the finding is gone and the exit code
   dropped to `0`.
4. Repeat until the scan is clean (or only `info` / `META` signals remain). For
   a stricter bar before committing, gate on `trustabl scan . --strict`.

## Installing the binary

This plugin runs a non-destructive `SessionStart` check
(`scripts/check-trustabl.sh`) that reports, into the session, when `trustabl` is
missing from `PATH` or older than the minimum the skills need. When you see a
`[trustabl] CLI not found …` or `… older than the minimum …` notice — or a scan
fails because `trustabl` isn't found — install it (from the project README),
**asking the user before you run any install command; never install silently**:

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
