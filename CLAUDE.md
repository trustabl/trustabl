# Instructions for Claude — Trustabl

This file captures durable architectural commitments that span the whole
codebase. Per-area conventions live in nested CLAUDE.md files (see
[`internal/rules/policies/CLAUDE.md`](internal/rules/policies/CLAUDE.md)
for rule authoring).

For the current implementation, see [`ARCHITECTURE.md`](ARCHITECTURE.md).
This file is for principles; ARCHITECTURE.md is for facts.

## Project naming

The product is named **Trustabl** (capital T). Use this spelling in all
human-facing prose: docs, status reports, finding messages shown in scan
reports, and CLI help text.

The lowercase `trustabl` is reserved for machine identifiers that must
**not** be capitalized: the binary name, the CLI command (`trustabl scan`,
`trustabl version`), the Go module path (`github.com/trustabl/trustabl`),
and internal prefixes (e.g. the clone temp-dir `trustabl-clone-*`). When in
doubt: if a human reads it as a sentence, it's "Trustabl"; if a machine
parses it as a token, it's `trustabl`.

## Detection model: three scopes

Every rule is classified into exactly one of three scopes. The `scope:`
field on a rule is REQUIRED for new rules; legacy rules without it default
to `tool` (the historical behavior).

- **`tool`** — fires per tool definition.
  - **Input**: a `ToolDef` (a `@function_tool`-decorated function, a
    `FunctionTool(...)` constructor call, a hosted-tool instance, a
    `@server.tool` MCP registration, or a bare shell-invoking function)
    plus its parsed file.
  - **Examples**: missing docstring, network call without timeout, untyped
    params, unnormalized path in `open()`.

- **`agent`** — fires per agent declaration.
  - **Input**: an `AgentDef` — a single `Agent(...)` /
    `SandboxAgent(...)` / Claude `AgentDefinition(...)` call — with all
    its kwargs captured and edges to its tools / handoffs / guardrails
    resolved.
  - **Examples**: agent has no `input_guardrails`,
    `tool_use_behavior="stop_on_first_tool"` paired with
    filesystem-touching tools, handoff to subagent that has fewer
    guardrails than the parent.

- **`repo`** — fires once per scan against the manifest.
  - **Input**: the `ScanManifest` (file inventory, dependency flags,
    discovered components).
  - **Examples**: project-wide tracing config has no custom processor;
    no `SandboxAgent` anywhere in a project that ships FS-touching tools.

What older code calls `singleton: true` is `repo` scope in disguise.
Promote to explicit `scope: repo` when touching those rules.

## Scanning pipeline

The scan is a flat sequence of steps; the output of each is the typed
input to the next. The boundary between the cheap recon step and the
AST-driven steps that follow is load-bearing: recon stays cheap so it can
gate whether the expensive AST work runs at all, and the inventory those
steps build is what makes policy selection data-driven rather than
statically configured.

### Step 1 — Recon (cheap, no AST)

Walk the repo and answer "what's in here" without parsing any source
language. Produces a `RepoProfile`:

- Languages present (by file extension).
- **SDK dependencies declared** — by text scan of `pyproject.toml` /
  `requirements.txt` / `Pipfile` / `poetry.lock` / `package.json` /
  `go.mod`. Each declaration becomes a typed `SDKDep{Name, Manifest,
  Confidence}`.
- File inventory (the existing `ScanManifest` work).
- Component discovery (MCP configs, hook scripts, CLAUDE.md, sandbox
  policies, etc.).
- A per-language "should we attempt the AST steps here" decision.

Recon must remain cheap. No tree-sitter parses here — those belong in the
inventory step.

### Step 2 — Inventory (per-language AST)

For each language recon cleared, do the AST work and extract a
`RepoInventory`:

- `ToolDef`s with **their config captured** — decorator kwargs
  (`strict_mode`, `failure_error_function`, hosted-tool args), function
  signature, docstring presence, body facts (touches FS, shells out,
  makes HTTP calls).
- `AgentDef`s with **all constructor kwargs captured as typed records**
  — instructions, model, model_settings, tools, handoffs,
  input_guardrails, output_guardrails, tool_use_behavior, mcp_servers,
  etc.
- `GuardrailDef`s (functions decorated `@input_guardrail` /
  `@output_guardrail`).
- `SessionUse` sites (where `SQLiteSession` / `RedisSession` / etc. are
  constructed).
- Edges: agent → tools, agent → handoffs, agent → guardrails. Resolved
  best-effort by in-file symbol lookup; unresolved references are
  flagged `external` rather than dropped.
- `SDKsDetected` — the set of SDKs *observed in code*, not just
  declared as deps.

The inventory is typed. Detectors read fields off Go structs — never
re-parse, never substring-match against raw source from inside a
detector.

### Step 3 — Policy selection (data-driven)

Based on `inventory.SDKsDetected`, decide which policy packs to load.

Rules:

- Load **only** the policy packs for SDKs that are observed in the
  inventory. Do not eagerly load every embedded YAML.
- For each SDK in `inventory.SDKsDetected` that has **no policy pack
  shipped**, emit one `info`-level finding: *"this repo uses SDK X,
  which Trustabl does not currently audit."* This is the honest
  unaudited signal — silence on an unknown SDK is wrong.
- For each SDK declared as a dep but with no observed code use, emit
  a different `info`-level finding noting the dep is unused (low
  priority — surfaces drift between deps and code).

### Step 4 — Analysis

Run the selected policy packs against the inventory. Detectors are
scope-aware (see the three-scope model below) and receive typed inputs:

- `tool`-scoped detectors receive a `ToolDef`.
- `agent`-scoped detectors receive an `AgentDef` with its resolved
  edges to tools, guardrails, and handoffs.
- `repo`-scoped detectors receive `RepoProfile` + `RepoInventory`.

Findings carry the scope they fired at, and attribute to the right
location: tool file/line, agent constructor call site, or the manifest.

### Why this staging matters

- **Performance.** Repos with no Python skip Python AST work. Repos
  with only Claude agents skip OpenAI policy loading.
- **Honest coverage.** An "unaudited SDK" info finding is louder than a
  zero-findings clean bill of health on an SDK we don't know about.
- **Determinism.** Each step's output is a structured artifact (Go
  struct, JSON-serializable) that can be logged, diffed, and tested in
  isolation.
- **Future SDKs slot in cleanly.** Adding a new SDK means: extend the
  recon dep-scan needles, extend the inventory-step discovery patterns for
  that SDK's tool/agent shapes, add a policy pack under
  `internal/rules/policies/<sdk>/`. No engine changes.

## Agent as the unit of analysis (not the repo)

A repo can declare zero, one, or many agents — across one or more SDKs in
the same repo. **Two agents in the same repo can be in completely
different security postures**: one wired with input/output guardrails, the
other not. Detection MUST attribute agent-scoped findings to a specific
agent. Flattening agent-scoped facts to a repo-level finding loses the
attribution and is incorrect.

Discovery therefore builds a small graph per repo:

1. **ToolDefs** — every tool definition in the repo.
2. **AgentDefs** — every agent declaration, with all kwargs captured as
   a structured record.
3. **Edges**:
   - `Agent.tools=[...]` → resolves to `ToolDef`s by best-effort
     in-file symbol lookup. Cross-module resolution is best-effort and
     cheap; unresolvable references are flagged `external` rather than
     dropped.
   - `Agent.handoffs=[...]` → resolves to other `AgentDef`s.
   - `Agent.input_guardrails` / `output_guardrails` → resolves to
     guardrail functions in the repo.

Agent-scoped rules query this graph; tool-scoped rules do not need it.

## SDK-scoped rules

Rules are scoped to a specific SDK AND language. Never widen `applies_to`
across SDKs casually — a rule's `explanation` and `fix` text is usually
SDK-specific. A Claude-SDK rule and an OpenAI-Agents-SDK rule that detect
the same conceptual problem (e.g. missing timeout) are TWO separate rules
in different policy files, each with framing that matches the target SDK.

This holds at all three scopes:
- Tool scope: `applies_to: [claude_sdk_tool]` vs `[openai_tool]`.
- Agent scope: `applies_to: [openai_agent]` vs `[claude_agent_definition]`.
- Repo scope: rules are organized by the SDK they target.

When a repo declares agents from multiple SDKs side by side, each agent
is checked against the rules for the SDK that declared it. No
cross-SDK casting.

## Doc precedence

When facts disagree across documentation:

1. **Code** is authoritative for *what the engine actually does today*.
2. **`internal/rules/schema.go`** is authoritative for the YAML schema
   (Go struct tags are the source of truth).
3. **`internal/rules/schema.yaml`** is the human reference for the schema
   — if it disagrees with `schema.go`, schema.go wins and schema.yaml is
   wrong, fix it.
4. **`ARCHITECTURE.md`** describes the current implementation.
5. **`README.md`** is the external-facing intro.
6. **`COVERAGE.md`** is the at-a-glance SDK/language coverage matrix.
7. **`.superpowers/specs/`** holds per-feature design docs (forward-
   looking; may not match current code). Local-only — `.superpowers/` is
   gitignored, so these won't exist in a fresh clone.
8. **`.superpowers/plans/`** holds in-flight implementation plans
   (ephemeral, may be stale). Local-only, same as the specs above.

When updating any of the above, check whether the change requires
updates to the others — especially `ARCHITECTURE.md` after a wiring
change, and `schema.yaml` after a schema change.

## Keeping documentation current

Documentation is part of the change, not a follow-up. Any change that
alters observable behavior MUST update the affected docs in the same
commit — stale docs are a defect, not a TODO. The three living docs and
their update triggers:

- **`ARCHITECTURE.md`** — update after any wiring change: a new or removed
  pipeline step, a new discovery shape, a changed data-model struct, or a
  moved package. It must always describe what the engine does *today*.
- **`README.md`** — update when the user-facing surface changes: CLI flags,
  exit codes, build steps, output formats, or the supported-SDK summary.
  Keep it honest — do not advertise capabilities that are not wired (e.g.
  LLM enrichment is opt-in and makes no call without a key).
- **`COVERAGE.md`** — update whenever SDK or language support changes: a new
  dep needle, a new discovery pattern, or a new rule pack. Re-derive the
  coverage matrix from the actual code, and bump the `_Last reviewed_` line
  to the current date and HEAD.

After any such edit, re-scan the precedence list above and reconcile any
downstream doc that now disagrees, in the same commit.

## Hard rules

For rule-authoring hard rules (rule IDs, severity, `applies_to`, schema
extension, test coverage), see
[`internal/rules/policies/CLAUDE.md`](internal/rules/policies/CLAUDE.md).
That file is the source of truth for the rule-authoring contract; do not
duplicate its rules here.

Repo-wide hard rules that span the whole codebase:

- **Determinism is a contract.** Same inputs → same `ScanID`, and a
  byte-stable report. Any ordered output (findings, inventory slices,
  components) MUST be sorted and deduped deterministically before
  emitting — no timestamps, map iteration order, or scheduling may leak in.
- **Never commit secrets, credentials, or example repos under
  `examples/`** without confirming the source is public and
  unencumbered. The examples corpus is part of the test contract — it
  is read by `scanner_test.go` on every test run.
- **Don't bypass discovery.** Detectors operate on `ToolDef` /
  `AgentDef` produced by `internal/analysis/discovery.go`. Do not
  re-walk the AST inside a detector to invent a new tool kind on the
  fly; extend discovery instead.

## Where to put planning artifacts

Per the global CLAUDE.md:
- Plans: `.superpowers/plans/<date>-<slug>.md`
- Specs: `.superpowers/specs/<date>-<slug>-design.md`

These are local-only — the `.superpowers/` directory is gitignored.
Status reports go to `docs/status-report-YYYY-MM-DD.txt` and are also
local-only (not committed).
