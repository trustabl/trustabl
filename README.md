<p align="center">
  <img src="assets/github_banner.jpg" alt="Trustabl" width="100%">
</p>

Trustabl is a static analyzer for agent reliability. It parses an agent-SDK
repository (Claude Agent SDK, OpenAI Agents SDK, Google ADK, MCP), models the
tools, agents, and subagents it declares, and checks them against a catalog
of reliability and safety rules. It reports the weaknesses it finds — each
with an explanation, a suggested fix, and a confidence score — as a
human-readable summary, JSON, or SARIF 2.1.0, plus a per-tool reliability
score and a CI-friendly exit code. It ships as a single Go binary; there is
no daemon, server, or hosted service.

The rest of this document explains *what Trustabl reasons about* and *how
the scan works*, then covers building and running it. For the full
implementation reference see [ARCHITECTURE.md](ARCHITECTURE.md); for the
at-a-glance SDK coverage matrix see [COVERAGE.md](COVERAGE.md).

## What it analyzes — the four-scope model

Trustabl does not treat a repository as one undifferentiated blob. Every
rule is classified into exactly one of four scopes, and each scope receives
a different typed input:

- **`tool`** — fires once per tool definition. Input: a `ToolDef` (a
  `@function_tool` / `@tool` / `@claude_tool` function, a Claude TS
  `tool(name, description, schema, handler)` factory call, a
  `FunctionTool(fn)` ADK wrapper, an `@server.tool` MCP registration, or a
  bare shell-invoking function) plus its parsed file. Catches a missing
  docstring, an HTTP call with no timeout, untyped parameters, or an
  unnormalized path flowing into `open()`. (Hosted tools like
  `WebSearchTool()` are agent-scope edge data, captured as `HostedToolDef`,
  not `ToolDef`.)
- **`agent`** — fires once per agent declaration. Input: an `AgentDef` —
  a Python `Agent(...)` / `SandboxAgent(...)` / `AgentDefinition(...)`
  call, a Claude TS typed-const `AgentDefinition`, a Claude TS sub-agent
  inline in `options.agents`, or the Claude TS `query(...)` main-thread
  agent (`QueryMainAgent`) — with every constructor kwarg captured and its
  edges to tools, handoffs, and guardrails resolved. Catches an agent with
  shell tools and no `input_guardrails`,
  `tool_use_behavior="stop_on_first_tool"` paired with filesystem-touching
  tools, or a main-thread agent with unrestricted `allowedTools`.
- **`subagent`** — fires once per `.claude/agents/*.md` declaration
  (matched at any path depth, monorepo-safe). Input: a `SubagentDef`
  parsed from markdown frontmatter (`name`, `description`, `tools[]`,
  `model`). Catches a subagent granted the built-in `Bash` tool despite
  a read-only description (CSDK-110).
- **`repo`** — fires once per scan against the whole inventory. Catches
  project-wide gaps such as the OpenAI Agents SDK being present with no
  custom trace processor configured.

### The agent is the unit of analysis, not the repo

A repo can declare zero, one, or many agents, across one or more SDKs.
**Two agents in the same repo can be in completely different security
postures** — one wired with input/output guardrails, the other not.
Agent-scoped findings therefore attribute to a *specific* agent at its
constructor call site; flattening them to a single repo-level verdict
would lose that attribution and be wrong. Discovery builds a small
per-repo graph (tools, agents, subagents, and the edges between them) so
agent-scope and subagent-scope rules can query it.

### Rules are scoped to one SDK *and* one language

A Claude-SDK rule and an OpenAI-Agents-SDK rule that detect the same
conceptual problem (a missing timeout, say) are two separate rules with
SDK-specific explanation and fix text — there is no cross-SDK casting.
When a repo declares agents from multiple SDKs side by side, each agent is
checked only against the rules for the SDK that declared it. The same
holds across languages: a `language: python` rule will not fire on a
TypeScript agent.

## How it reasons — the scanning pipeline

trustabl scans in four steps. Each step's output is the typed input to the
next, with no shared state between runs — and the inventory the early
steps build is what makes policy selection *data-driven* rather than
statically configured.

The binary ships with **no embedded rules**. Before the pipeline runs,
Trustabl resolves its detection rules from a separate git repository
([`trustabl-rules`](https://github.com/trustabl/trustabl-rules)) —
fetching the latest, caching the clone locally, and falling back to the
cache when the network is unreachable. This decouples rule updates from
binary releases: rules can be added or changed without rebuilding the
scanner. The resolved rules commit is recorded in the result and folded
into the `ScanID`, so a scan is honest about *which* rules produced it.
If no rules can be fetched and none are cached, the scan exits `2` and
tells you to run `trustabl rules pull` — Trustabl never runs rule-less.

```mermaid
flowchart LR
    target[("Agent repo<br/>(local path or GitHub URL)")]
    recon["Recon<br/>files · SDK deps"]
    inv["Inventory<br/>Python + TS AST:<br/>tools · agents ·<br/>subagents · MCP servers"]
    pol["Policy selection<br/>load rules per<br/>detected SDK ·<br/>META findings"]
    ana["Analysis<br/>tool · agent · subagent ·<br/>repo detectors"]
    score["Scoring<br/>per-tool score ·<br/>overall readiness"]
    out[("ScanResult<br/>findings · scores<br/>(human / JSON / SARIF)")]

    target --> recon --> inv --> pol --> ana --> score --> out
```

1. **Recon** — walk the repo and answer "what's in here" cheaply, without
   parsing any source language: languages present (by extension), SDK
   dependencies declared in manifests (`pyproject.toml` / `requirements.txt`
   / `Pipfile` / `poetry.lock` / `package.json` for the
   `claude-agent-sdk` / `@anthropic-ai/claude-agent-sdk` / `openai-agents` /
   `@openai/agents` / `google-adk` needles), the file inventory, and
   discovered agent components (MCP configs, hook scripts, `CLAUDE.md`,
   `.claude/agents/*.md` subagents at any depth, sandbox policies). No
   tree-sitter parses happen here — this step decides whether the
   expensive AST work is even worth attempting.
2. **Inventory** — for each language Recon cleared, do the AST work and
   extract a typed inventory: `ToolDef`s with their config and body facts,
   `AgentDef`s with all kwargs captured, `SubagentDef`s from markdown
   frontmatter, `MCPServerDef`s, guardrails, sessions, and the resolved
   edges between agents and the tools/guardrails they reference. Detectors
   read fields off these structs — they never re-parse raw source.
3. **Policy selection** — load **only** the rule packs for SDKs actually
   *observed in code*. An SDK seen in code with no shipped pack emits a
   `META-001` info finding ("Trustabl does not currently audit this SDK")
   — silence on an unknown SDK is wrong. A dep declared but never used in
   code emits a different info finding flagging the drift.
4. **Analysis** — run the selected scope-aware detectors against the
   inventory. Findings carry the scope they fired at and attribute to the
   right location: tool file/line, agent call site, subagent markdown
   file, or the manifest.

Three properties fall out of this staging, by design:

- **Performance.** A repo with no Python skips Python AST work; a repo
  with only Claude TS code skips Python AST work AND OpenAI policy
  loading.
- **Honest coverage.** An "unaudited SDK" info finding is louder than a
  zero-findings clean bill of health on an SDK Trustabl doesn't know. A
  `META-004` finding further distinguishes "audited and clean" from
  "could not audit — discovery extracted nothing a rule targets."
- **Determinism is a contract.** Same inputs → same `ScanID`, and the
  report is byte-stable across runs (findings sorted by
  `(RuleID, FilePath, Line)`, inventory slices sorted deterministically).
  CI consumers can diff scans without spurious churn.

See [ARCHITECTURE.md § 2](ARCHITECTURE.md#2-pipeline) for the full
diagram with typed inputs at each step.

### What's wired today

Tool/agent AST discovery is wired for:

- **Python** — Claude Agent SDK (decorators), OpenAI Agents SDK, Google
  ADK. Discovery extracts tool definitions, agent constructors, hosted
  tools, MCP servers, guardrails, sessions.
- **TypeScript** — Claude Agent SDK (the `tool()` factory, the
  `query()` main-thread `QueryMainAgent`, inline-in-`query()` sub-agents,
  typed-const `AgentDefinition`s, `createSdkMcpServer` and the four
  `options.mcpServers` config literals). Handles `.ts` / `.tsx` / `.mts`
  / `.cts` with both `tree-sitter-typescript` and `tree-sitter-tsx`
  grammars. Note: no TypeScript-specific rule pack ships yet; the engine
  detects TS Claude SDK shapes but the inventory lands as `META-004`
  findings until SP2 ships TS rules.

JavaScript and Go files are recognized by Recon (they appear in the
file inventory and feed component discovery) but no AST parser for them
is wired in, so no tools or agents are extracted from them. The rule
schema's `language:` field is in place for when those parsers ship.

### Scope boundaries

- **LLM enrichment is opt-in.** The BYOK interface and cache exist
  (`internal/inference/router.go`), but rule-based detection runs fully
  without a key and makes no network call without one.
- **Confidence scores are heuristic**, not LLM-judged, and not yet
  calibrated against a labelled real-agent corpus — treat findings as
  signal to investigate.
- **The CLI is the surface.** No web app, API server, or GitHub Action —
  pipe `--format json` or `--format sarif` into your own automation.

## What it produces

Trustabl is a detect-and-report tool: it does **not** write or modify any
files in the scanned repo. Each run produces a `ScanResult` containing:

- **Findings** — one per rule hit, each with `severity`, `confidence`,
  an `explanation`, a `suggested_fix`, and the location it fired at
  (tool file/line, agent call site, subagent file, or the manifest).
- **Per-tool readiness scores** (over custom tool definitions, since
  those are what tool-scope rules audit) and an **overall score** (the
  minimum across tools — an agent is only as reliable as its weakest
  surface).
- **The discovered inventory** — tools, agents, hosted tools, MCP
  servers, subagents, and Claude settings — surfaced at the top level
  for CI consumers.

### The summary's tool surface, broken out

The human format honestly separates the three things people commonly
conflate:

```
Tool definitions:   2  (custom tools with function bodies — scored below)
Agent tool grants: 14  (tool names the agent may call — audited by agent-scope rules)
Hosted tools:       1  (...)
```

Only the "Tool definitions" category flows through tool-scope rules
(they have function bodies a rule can read). Agent grants and hosted
instances are inputs to *agent-scope* rules, not unanalyzed — they just
don't appear in the per-tool readiness table.

### Output modes

`--format human` (default) renders a human summary to stdout and live
progress to stderr — an animated spinner and progress bar on an
interactive terminal, or plain `[phase] summary` lines when piped
(CI-friendly).

`--format json` marshals the full `ScanResult` for piping into your
own automation.

`--format sarif` emits a SARIF 2.1.0 document, suitable for
`github/codeql-action/upload-sarif` and other SARIF-aware tools.

`--format json` and `--format sarif` are progress-silent. All three
formats are byte-stable across identical-input runs.

Exit codes:
- `0` — no findings ≥ medium severity (or no findings at all).
- `1` — at least one finding ≥ medium severity, OR `--strict` with any
  finding present.
- `2` — scanner / I/O error, OR no usable rules found and none fetchable
  (run `trustabl rules pull`).

OpenShell surfaces are still discovered (shell-invocation functions,
`openshell/*.yaml` policies) and reported on a `Risk surfaces: openshell`
line, but the OSH-* detection rules that audited them have moved to a
closed-source companion project. With no OSH rules shipped, such repos fire
no rule and no `META` finding — OpenShell is a risk surface, not an SDK, so
it is not flagged as "unaudited" the way an unknown SDK would be.

## Install

### Homebrew (macOS, Linux)

```sh
brew install trustabl/tap/trustabl
```

### Scoop (Windows)

```sh
scoop bucket add trustabl https://github.com/trustabl/scoop-bucket
scoop install trustabl
```

### Docker

```sh
docker run --rm -v "$PWD:/repo" ghcr.io/trustabl/trustabl:latest scan /repo
```

### Direct download

Grab a prebuilt archive for your platform from the
[Releases page](https://github.com/trustabl/trustabl/releases). Each release
includes a `checksums.txt` and a build-provenance attestation; verify with:

```sh
gh attestation verify <archive> --repo trustabl/trustabl
```

### From source

Requires `CGO_ENABLED=1` because the AST parsers use tree-sitter
(Python + TypeScript + TSX bindings), which is a C library:

```bash
# macOS / Linux
CGO_ENABLED=1 go build -o trustabl ./cmd/trustabl

# Cross-compile: pick a C toolchain for the target. zig is the easiest.
CGO_ENABLED=1 CC="zig cc -target x86_64-linux-gnu" \
  GOOS=linux GOARCH=amd64 go build -o trustabl-linux ./cmd/trustabl
```

This is the cost of using tree-sitter for accurate AST parsing. If a
single-binary, no-CGO distribution becomes a hard requirement later, the
swap target is `github.com/go-python/gpython` for Python (with lower
fidelity on modern Python); TypeScript would need a separate replacement.

## Use

```bash
# Local repo
trustabl scan ./path/to/agent-repo

# GitHub repo (shallow clone to temp dir, removed on exit)
trustabl scan https://github.com/org/repo

# Restrict detectors
trustabl scan ./repo --detectors claude_sdk
trustabl scan ./repo --detectors openai_sdk
trustabl scan ./repo --detectors google_adk
trustabl scan ./repo --detectors claude_sdk,openai_sdk,google_adk
# --detectors openshell is accepted but selects zero rules (pack is closed-source now)

# JSON output for CI piping
trustabl scan ./repo --format json

# SARIF output for GitHub Code Scanning / SARIF-aware tools
trustabl scan ./repo --format sarif > trustabl.sarif

# Exit 1 on any finding regardless of severity
trustabl scan ./repo --strict

# Download / refresh the detection rule packs into the local cache
trustabl rules pull

# Use a custom rules repo or a specific ref (env: TRUSTABL_RULES_REPO)
trustabl scan ./repo --rules-repo https://github.com/org/my-rules
trustabl scan ./repo --rules-ref v1.2.0

# Air-gapped / offline: skip the network fetch, use the cached rules only
trustabl scan ./repo --no-rules-update

# Progress output (human format): animated on a terminal, plain lines when piped
trustabl scan ./repo                 # spinner + bars on a TTY; "[phase] summary" lines when piped
trustabl scan ./repo --no-progress   # disable progress entirely
```

Rules are cached under your OS cache dir (`os.UserCacheDir()`, e.g.
`%LocalAppData%\trustabl\rules\` on Windows, `~/.cache/trustabl/rules/`
on Linux). The first scan (or an explicit `trustabl rules pull`)
populates it; each subsequent scan checks for an update first (unless
`--no-rules-update`), falling back to the cached rules if the fetch
fails.

## Where the code lives

| Pipeline node      | Code path                                |
| ------------------ | ---------------------------------------- |
| Importer           | `internal/ingestion/importer.go`         |
| Normalizer (recon) | `internal/ingestion/normalizer.go`       |
| Python discovery   | `internal/analysis/discovery.go`, `agents.go`, `hosted_tools.go`, `mcp_servers.go`, `subagents.go`, `claude_settings.go`, `adk_agents.go` |
| TypeScript discovery | `internal/analysis/ts_discovery.go`, `ts_agents.go`, `ts_mcp_servers.go`, `astutil/ts.go` |
| Detector runtime   | `internal/analysis/detectors/`           |
| Rule source        | `internal/rulesource/` (git fetch + cache + schema-version gate) |
| Detector rules     | external `trustabl-rules` repo (tests: `testdata/rules-fixture/`) |
| Rule engine        | `internal/rules/{schema,loader,evaluator,predicates,rule_detector}.go` |
| Scoring engine     | `internal/analysis/scoring.go`           |
| Report renderer    | `internal/review/diff.go` (human), `internal/sarif/render.go` (SARIF), JSON marshal in `cmd/trustabl` |
| Inference router   | `internal/inference/router.go`           |

Rule packs live in the separate `trustabl-rules` git repository (grouped
`{claude_sdk,openai_sdk,google_adk}/`), resolved at scan time rather
than embedded in the binary. Naming convention: `CSDK-NNN` for Claude
Agent SDK rules (CSDK-0xx tool-scope, CSDK-1xx agent + subagent-scope),
`OAI-NNN` for OpenAI Agents SDK rules, `ADK-NNN` for Google ADK rules.
See
[ARCHITECTURE.md § 2 — steps 3–4](ARCHITECTURE.md#2-pipeline) for the
shipped rule table and [COVERAGE.md](COVERAGE.md) for per-SDK
recognition detail.

## Testing

`examples/` holds real-world agent code (Claude SDK demos, OpenAI Agents
SDK demos, Google ADK demos, a TS Claude SDK fixture) — a corpus, not a
controlled fixture, so well-written agents won't trigger most rules and
that's correct. See [`examples/PROVENANCE.md`](examples/PROVENANCE.md)
for upstream sources and licenses of each example. Per-rule fire/silent
correctness lives in `internal/rules/policies_test.go`; the end-to-end
sweep in `internal/scanner/scanner_test.go` only asserts the scanner
doesn't crash on real-world inputs. A labelled 20–40 real-agent-repo
corpus is the detection-quality target (see
[ARCHITECTURE.md § 10](ARCHITECTURE.md#10-what-is-intentionally-out));
the current tests are regression coverage, not detection-quality
measurement.

## License

Apache-2.0. See [`LICENSE`](LICENSE).
