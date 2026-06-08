# Instructions for Claude — Trustabl

This file captures durable architectural commitments that span the whole
codebase. Per-area conventions live in nested CLAUDE.md files (see
[`testdata/rules-fixture/CLAUDE.md`](testdata/rules-fixture/CLAUDE.md)
for rule authoring). The detection rule packs do **not** live in this repo:
they live in the external **`trustabl-rules`** repository
(`https://github.com/trustabl/trustabl-rules`), which the engine pulls at
scan time. `testdata/rules-fixture/` is an in-engine **test mirror** of those
packs — see [Two-repo rule model](#two-repo-rule-model-rules-vs-engine) below,
which is required reading before touching any rule.

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

## Detection model: five scopes

Every rule is classified into exactly one of five scopes. The `scope:`
field is REQUIRED on every rule — the loader rejects a rule with no
`scope:` (there is no longer a default-to-`tool` fallback; that historical
behavior was removed when the loader adopted strict decoding).

- **`tool`** — fires per tool definition.
  - **Input**: a `ToolDef` — discovery produces these from a
    `@function_tool`-decorated function (`openai_tool`), a `@tool` /
    `@claude_tool` / `claude_agent_sdk` function (`claude_sdk_tool`), the
    Claude TS SDK `tool(...)` factory call (also `claude_sdk_tool`), a
    `@server.tool` / `@mcp.tool` / `.register_tool` MCP registration
    (`mcp_tool`), or a bare shell-invoking function (`shell_invocation`) —
    plus its parsed file. (Hosted-tool instances like `WebSearchTool()` are
    captured as `HostedToolDef`, not `ToolDef`, and are agent-scope edge data,
    not tool-scope inputs.)
  - **Examples**: missing docstring, network call without timeout, untyped
    params, unnormalized path in `open()`.

- **`agent`** — fires per agent declaration.
  - **Input**: an `AgentDef` — a single `Agent(...)` /
    `SandboxAgent(...)` / Claude Python `AgentDefinition(...)` call, a
    Claude TS typed-const `AgentDefinition`, a Claude TS sub-agent inline
    in `options.agents`, or the TS `query(...)` main-thread agent
    (`QueryMainAgent`) — with all its kwargs captured and edges to its
    tools / handoffs / guardrails resolved.
  - **Examples**: agent has no `input_guardrails`,
    `tool_use_behavior="stop_on_first_tool"` paired with
    filesystem-touching tools, handoff to subagent that has fewer
    guardrails than the parent.

- **`subagent`** — fires per `.claude/agents/*.md` declaration.
  - **Input**: a `SubagentDef` parsed from markdown frontmatter (`name`,
    `description`, `tools[]`, `model`). Matched at any path depth
    (monorepo-safe). Carries no `Language` field — markdown frontmatter is
    language-agnostic, so subagent rules carry no `language:` field either
    and the detector does not gate on language.
  - **Examples**: subagent granted `Bash` despite a read-only description
    (CSDK-110), description-vs-tools mismatch, no `name`.

- **`skill`** — fires per `SKILL.md` declaration.
  - **Input**: a `SkillDef` parsed from `SKILL.md` frontmatter (`name`,
    `description`, `allowed-tools[]` → `ToolGrants`,
    `disable-model-invocation`) plus body facts (dynamic-context exec
    commands, external URLs, prompt-injection markers) and a bundled-file
    inventory. Matched at any path depth (monorepo-safe). Carries no
    `Language` field — `SKILL.md` is markdown, so skill rules carry no
    `language:` field either and the detector does not gate on language.
  - **Examples**: skill auto-approves unrestricted `Bash` (CSKILL-001), a
    dynamic-context command performs network egress or reads secrets
    (CSKILL-003), model-invocable skill grants side-effecting tools
    (CSKILL-050).

- **`repo`** — fires once per scan against the whole repo.
  - **Input**: `RepoProfile` + `RepoInventory` (languages, declared SDK
    deps, the `ScanManifest` file inventory and discovered components, plus
    the discovered tools/agents and `SDKsDetected`).
  - **Examples**: project-wide tracing config has no custom processor;
    no `SandboxAgent` anywhere in a project that ships FS-touching tools.

What older code called `singleton: true` is `repo` scope. The `singleton`
field no longer exists in the schema, and the loader uses strict decoding
(`KnownFields(true)`), so a `singleton:` key is now a hard load error — use
`scope: repo` instead.

## Scanning pipeline

The scan is a flat sequence of steps; the output of each is the typed
input to the next. The boundary between the cheap recon step and the
AST-driven steps that follow is load-bearing: recon stays cheap so it can
gate whether the expensive AST work runs at all, and the inventory those
steps build is what makes policy selection data-driven rather than
statically configured.

Before the pipeline runs, the CLI resolves detection rules from the
external `trustabl-rules` git repository (`rulesource.DefaultRepoURL`,
currently `https://github.com/trustabl/trustabl-rules`; the engine embeds
none — see `internal/rulesource/`) and hands them to `scanner.Run` as an
`fs.FS`. The
resolution path fetches the configured ref, caches the clone under
`os.UserCacheDir()/trustabl/rules/<sha>/`, and falls back to the cache when
the network is unreachable. Rule loading is **forward-compatible**: a pack
whose `manifest.yaml` `schema_version` exceeds the engine's
`rules.SupportedSchemaVersion` is loaded leniently rather than refused — a rule
referencing a `scope`, an `applies_to` value, a `language`, or a predicate this
build lacks is
skipped (recorded on `ScanResult.RulesSkipped`, warned on stderr, and summarized
in a `META-005` info finding) and the scan runs the rest. A malformed *known*
rule — empty/missing required field, out-of-range confidence, duplicate ID — is
**not** forward-incompatible and still hard-fails, in both strict and lenient
loading. A
hard exit 2 happens only when *nothing* is usable: no pack cached or fetchable
(`ErrNoRules`), an unreadable manifest (`ErrNoCompatibleRules`), a genuinely
empty pack (`ErrNoRulesInPack`), or one whose every rule is forward-incompatible
(`ErrAllRulesIncompatible`). The engine never runs rule-less. The resolved rules
SHA is recorded on `ScanResult` and folded into `ScanID` (with the engine's
`SupportedSchemaVersion`). See `internal/rules/schema_version.go` for the
bump/rename discipline this enables.

### Step 1 — Recon (cheap, no AST)

Walk the repo and answer "what's in here" without parsing any source
language. Produces a `RepoProfile`:

- Languages present (by file extension).
- **SDK dependencies declared** — by text scan of `pyproject.toml` /
  `requirements.txt` / `Pipfile` / `poetry.lock` / `package.json` /
  `go.mod`. Each declaration becomes a typed `SDKDep{Name, Source,
  Confidence}` (`Source` is the manifest file the declaration came from).
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
scope-aware (see the five-scope model above) and receive typed inputs:

- `tool`-scoped detectors receive a `ToolDef`.
- `agent`-scoped detectors receive an `AgentDef` with its resolved
  edges to tools, guardrails, and handoffs.
- `subagent`-scoped detectors receive a `SubagentDef` parsed from
  `.claude/agents/*.md` frontmatter.
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
  that SDK's tool/agent shapes, add a policy pack under `<sdk>/` in the
  external `trustabl-rules` repository. No engine changes, no rebuild.

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

This holds at all five scopes:
- Tool scope: `applies_to: [claude_sdk_tool]` vs `[openai_tool]`.
- Agent scope: `applies_to: [openai_agent]` vs `[claude_agent_definition]`.
- Repo scope: rules are organized by the SDK they target.
- Skill scope: `applies_to: [claude_skill]` — Claude-only, no cross-SDK
  equivalent, and skills carry no `language:`.

When a repo declares agents from multiple SDKs side by side, each agent
is checked against the rules for the SDK that declared it. No
cross-SDK casting.

## Three-repo rule model (engine vs rules vs rulebook)

Trustabl is split across **three repositories**. Internalize this before
touching rules — getting it wrong silently ships untested rules, test-passes
rules users never receive, or ships rules with no defensible grounding behind
them.

- **Engine repo** (this one, `github.com/trustabl/trustabl`): the scanner
  binary. Owns discovery, the rule **schema** (`internal/rules/schema.go` +
  `schema.yaml`), predicates, the evaluator, the loader, scoring, and the
  per-rule test harness. Ships with **no rules embedded**.
- **Rules repo** (`https://github.com/trustabl/trustabl-rules`, set as
  `rulesource.DefaultRepoURL`): the `.yaml` rule packs + `manifest.yaml`.
  This is what `trustabl scan` clones and runs at scan time. It is the
  **production source of rules**.
- **Rulebook repo** (`https://github.com/trustabl/trustabl-rulebook`):
  **documentation only, ships no YAML rules.** This is the **home for every
  rule's defensible grounding** — one rationale doc per YAML pack at
  `docs/Policy/<category>/<topic>.md`, each carrying the threat model,
  citations, severity/confidence defense, and known gaps. See
  [Rule grounding lives in the rulebook, not the schema](#rule-grounding-lives-in-the-rulebook-not-the-schema)
  below. Local sibling checkout: `../trustabl-rulebook`.

### Rule grounding lives in the rulebook, not the schema

**Do NOT add a `references` / `cwe` / `owasp` / citation field to `RuleDef`.**
The rule schema is intentionally lean — `RuleDef` carries only ID, Title,
Scope, Severity, Confidence, Language, AppliesTo, Match, Explanation, Fix.
Detection rules answer *"does this pattern exist"* (proven mechanically by the
AST predicate); they deliberately do not carry the evidence for *"why this
pattern is dangerous."* That evidence — the citations and threat model that
defend a finding against a skeptical researcher — lives entirely in the
**rulebook rationale doc**, which has the room and structure for it that a YAML
field never would.

Each rationale doc has machine-checked YAML front-matter (`policy_id`,
`category`, `topic`, and a `rules:` list whose `severity`/`confidence`/`scope`
**must equal the shipped YAML** — `tools/check_rulebook.py` fails CI on
divergence, on a shipped rule with no doc, and on a documented rule that no
longer exists). It then defends each rule in prose: what we detect (mapped to
the actual predicate), why it is flaggable (mechanism, not assertion), the
real-world consequence, a severity defense, a confidence-gap analysis, an
adversarial "what this does not cover" (FP/evasion scenarios), and a full
safe-code recommendation. `references` are **OWASP LLM Top 10:2025 IDs**
(`LLM01`–`LLM10`, a closed set) plus an editorial `fix_type` (`config` |
`code`); both are editorial-only — the engine does not model them, so the gate
validates their shape but cannot cross-check them. The authoring contract is
[`docs/policy-rationale-doc-template-guide.md`](https://github.com/trustabl/trustabl-rulebook)
in that repo.

Inside the engine repo, **`testdata/rules-fixture/`** is a copy of the rules
repo's packs, injected via `os.DirFS` so `go test` can validate rules without
network access. It is the **test mirror**, not the source of truth for what
users get — production pulls the live rules repo, not the fixture.

### The sync obligation

Because the fixture and the live rules repo are two copies, **they drift unless
deliberately kept in sync.** A rule that exists only in the fixture is tested
but never shipped; a rule that exists only in the rules repo ships but is
untested (the engine's `TestPolicyRules_AllRulesCovered` guard only sees the
fixture). Both are defects.

This drift is now caught automatically: the `rules-sync` CI job
(`.github/workflows/test.yml`) checks out `trustabl-rules` and runs
[`scripts/check-rules-sync.sh`](scripts/check-rules-sync.sh), which fails the
build on any fixture↔production divergence (a fixture-only file, a
production-only file, or content drift — line endings ignored). Run it locally
with `RULES_REPO=../trustabl-rules scripts/check-rules-sync.sh` before pushing a
rule change to either repo.

When changing a rule (add / remove / edit severity, confidence, match, text):

1. Make the change in the **rules repo** (`../trustabl-rules/`) — that is what
   users pull.
2. Mirror the identical change into **`testdata/rules-fixture/`** in this repo.
3. Add/update the rule's fire + silent cases in
   `internal/rules/policies_test.go` (the `TestPolicyRules_AllRulesCovered`
   guard fails the build if a fixture rule has no case).
4. **Update the paired rulebook rationale doc** in `../trustabl-rulebook/`
   (`docs/Policy/<category>/<topic>.md`) — its front-matter
   `severity`/`confidence`/`scope` must match the new YAML, and a *new* rule
   needs a full rule-by-rule defense block with its OWASP `references`. The
   rulebook's `tools/check_rulebook.py` gate fails on a shipped rule with no
   doc. A rule shipped without grounding is the defect this whole model exists
   to prevent.
5. `go test ./...` here must pass.
6. Commit and push the rules repo **and** the rulebook (the user pushes engine
   commits manually; confirm before pushing any of the three).

> **Rulebook status (2026-06-06):** production ships **165** rules across nine
> SDK categories (`autogen`, `claude_sdk`, `crewai`, `google_adk`, `langchain`,
> `mcp`, `openai_sdk`, `pydantic_ai`, `vercel_ai`). The rulebook carries a
> rationale doc for every shipped rule except the newly-added VAI-011 (82 docs);
> `check_rulebook.py` flags that single gap until the vercel network rationale
> doc lands.

The rule-authoring contract (required fields, ID conventions, per-scope
`applies_to` values, framing discipline) lives in
[`testdata/rules-fixture/CLAUDE.md`](testdata/rules-fixture/CLAUDE.md) and the
mirror copy in the rules repo. Schema changes (a new predicate or match field)
are **engine-repo** changes (schema.go + predicates.go + evaluator.go +
schema.yaml in one commit) and bump `SupportedSchemaVersion` + `manifest.yaml`'s
`schema_version` in both the fixture and the rules repo. With forward-compatible
loading that bump *advertises* this build's support level (and drives the "rules
newer than this build" warning) — it is **not** a fleet-wide gate: an older
binary loads a newer pack leniently, skipping only the rules it cannot evaluate.
Make **breaking** changes by *renaming* a predicate, never by silently
redefining one — an old binary then skips the unknown-key rule rather than
mis-evaluating it. See
[`internal/rules/schema_version.go`](internal/rules/schema_version.go).

> **Future direction (not yet done):** the duplication between the fixture and
> the live rules repo is a known cost. The intended end state points the test
> harness directly at a vendored/submoduled rules checkout so there is one
> source of truth. Until that lands, the manual sync above is the contract.

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
[`testdata/rules-fixture/CLAUDE.md`](testdata/rules-fixture/CLAUDE.md).
That file is the source of truth for the rule-authoring contract; do not
duplicate its rules here.

Repo-wide hard rules that span the whole codebase:

- **Determinism is a contract.** Same inputs → same `ScanID`, and a
  byte-stable report. `ScanID` folds the resolved rules version, so the ID
  is honest about which rule pack produced the scan — a different pack
  yields a distinct ID. Any ordered output (findings, inventory slices,
  components) MUST be sorted and deduped deterministically before
  emitting — no timestamps, map iteration order, or scheduling may leak in.
  Real-time progress output (the `internal/progress` package) is **stderr-only**
  and must never write to stdout or influence `ScanResult` — the report stays
  byte-stable regardless of progress mode.
- **Never commit secrets, credentials, or example repos under
  `testdata/corpus/`** without confirming the source is public and
  unencumbered. The corpus is part of the test contract — it
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
