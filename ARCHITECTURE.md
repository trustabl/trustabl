# Architecture

This document describes the concrete architecture of the karenctl codebase as it
exists today. It is the implementer's reference: what each package owns, the
data that crosses package boundaries, and the decisions that shaped the layout.

For the *product* vision (Trustabl Strawman v1), see the design doc tracker —
this file is scoped to the Go binary in this repository.

---

## 1. Goal

karenctl scans a Claude Agent SDK repository, finds reliability weaknesses in
its tool definitions, and emits committable artifacts that close those gaps:

- `hooks/pretooluse_validate.py` and `hooks/posttooluse_log.py` — Claude Agent
  SDK hook scripts the user commits to their own repo.
- `openshell/policy.yaml` — an NVIDIA OpenShell sandbox policy gating the
  agent's runtime privileges.

Single Go binary, no daemon, no server. Web app and CI surfaces are out of
scope for this skeleton (see `README.md` § Status).

---

## 1.1 Language scope

karenctl ships with **Python tool discovery** wired in. The scanner can also
recognize TypeScript, JavaScript, and Go *files* (they appear in
`manifest.typescript_files` etc. and feed component discovery), but no AST
parser for those languages is plumbed in yet — so no tools are extracted
from them and no rules fire against them.

The rule schema's `language:` field gates per-language rule sets. Existing
rules declare `language: python` explicitly and the loader rejects any
unknown language value. When TypeScript tool discovery lands, new rules
declare `language: typescript` and run only against TS tools; Python rules
remain inert against TS tools.

Adding a new tool-discovery language requires:

1. A tree-sitter binding for that language in `internal/analysis/astutil/`.
2. Discovery patterns for that language's tool definitions in
   `internal/analysis/discovery.go` (e.g. AI SDK's `tool({})` factory in TS).
3. Per-language predicate implementations in `internal/rules/predicates.go`
   (since AST node types differ across languages).
4. New rule files under `internal/rules/policies/<category>/` declaring
   `language: <new>`.

The OpenShell policy generator is already language-agnostic — that
infrastructure carries over for free when language #2 ships.

---

## 2. Pipeline

The scan is a one-shot pipeline. There is no concurrency between stages and no
state shared across runs. `scanner.Run` ([internal/scanner/scanner.go](internal/scanner/scanner.go))
calls each stage in order; the output of one is the input to the next.

```
target (path or URL)
    │
    ▼
┌──────────────┐
│  Importer    │  ingestion.Resolve   → *Source       (clones if remote)
├──────────────┤
│  Normalizer  │  ingestion.Normalize → ScanManifest  (file inventory)
├──────────────┤
│  Discovery   │  analysis.DiscoverTools → []ToolDef + []ParsedFile
├──────────────┤
│  Detectors   │  detectors.Registry.Run → []Finding
├──────────────┤
│  Scoring     │  analysis.Score → []ToolReadiness, OverallScore
├──────────────┤
│  Generation  │  generation.GenerateHooks + GeneratePolicy → []GeneratedArtifact
├──────────────┤
│  Review      │  review.Renderer / ApplyArtifacts / ExportZIP
└──────────────┘
    │
    ▼
ScanResult  (JSON-serializable, returned to the CLI)
```

### Stage 1 — Importer ([internal/ingestion/importer.go](internal/ingestion/importer.go))

Resolves a CLI target string to a directory on disk. Local paths pass through;
URLs and `git@host:owner/repo` shorthand are shallow-cloned to a temp dir using
`go-git`. The returned `Source` carries a `Cleanup()` callback the caller MUST
defer; for local targets it is a no-op so call sites don't need a branching
defer.

### Stage 2 — Normalizer ([internal/ingestion/normalizer.go](internal/ingestion/normalizer.go))

Walks the source tree, collecting per-language file paths
(`PythonFiles`, `TypeScriptFiles`, `JavaScriptFiles`, plus `YAMLFiles`,
`JSONFiles`, `MarkdownFiles`) and producing two kinds of metadata:

**Manifest-level signals.**

- `HasClaudeSDKDependency` — text search in pyproject.toml / requirements.txt /
  Pipfile / poetry.lock for SDK markers.
- `HasOpenShellArtifact` — presence of an `openshell/` dir or a YAML file
  declaring `openshell.nvidia.com/v…`.

**Discovered agent components** (`Components []AgentComponent`).

The normalizer enumerates non-tool agent artifacts so users see the full
agent surface, even though detection rules currently only run against
tools. Component kinds:

| Kind                  | What it matches                                                |
| --------------------- | -------------------------------------------------------------- |
| `mcp_config`          | `mcp.json`, `mcp_servers.json`, `claude_desktop_config.json`   |
| `claude_md`           | `CLAUDE.md` / `claude.md` at any depth                         |
| `claude_settings`     | `.claude/settings.json`, `.claude/settings.local.json`         |
| `subagent`            | `.claude/agents/*.md`                                          |
| `slash_command`       | `.claude/commands/*.md`                                        |
| `hook_script`         | `hooks/*.{py,ts,js,jsx,mjs}`                                   |
| `sandbox_policy`      | `openshell/*.yaml` / `openshell/*.yml`                         |
| `system_prompt`       | `prompts/*.md`, `system_prompt.md`, `system_prompt.txt` (root) |
| `dependency_manifest` | `pyproject.toml`, `requirements.txt`, `Pipfile`, `poetry.lock`, `package.json`, `go.mod` |

Each `AgentComponent` carries `Path` (relative to repo root, normalized to
forward slashes) and `Language` (set for code components, empty for
configs / prompts).

**Directory skip rules.** Skips `.git`, `.venv`, `venv`, `node_modules`,
`__pycache__`, `dist`, `build`, `.tox`, `.mypy_cache`, `.pytest_cache`, and
any other dot-prefixed directory — **except `.claude/`**, which is a
deliberately-included agent-config directory.

Manifest fields are emitted as JSON in `ScanResult.manifest` for CI consumers;
the Go pipeline does not currently branch on them.

### Stage 3 — Tool Discovery ([internal/analysis/discovery.go](internal/analysis/discovery.go))

Two-pass discovery over each Python file. tree-sitter is used because we need
structural recognition (decorator nodes, function bodies, call shapes) rather
than just text matching.

1. **Decorated functions.** Any `decorated_definition` whose decorator text
   contains `@tool`, `@claude_tool`, `@agent.tool`, `claude_agent_sdk` is a
   `KindClaudeSDKTool`. `@server.tool`, `@mcp.tool`, `.register_tool` is a
   `KindMCPTool`. These signals are conservative — when in doubt, return
   `KindUnknown` and let the function be considered for shell discovery.
2. **Bare functions that shell out.** Any `function_definition` not already
   captured above whose body calls `subprocess.*`, `os.system`, or `os.popen`
   is a `KindShellInvocation`. These feed the OpenShell detectors.

The function's docstring is extracted via `astutil.FunctionDocstring`, which
calls `stripPythonStringLiteral` to handle prefixes (r/b/u/f and 2-char
combinations) and triple-vs-single quote markers. Parameter names come from
`astutil.FunctionParams`; `self`/`cls` are dropped. `HasTypedParams` is set if
any parameter is type-annotated (`typed_parameter` or `typed_default_parameter`
in tree-sitter terms).

### Stage 4 — Detectors ([internal/rules/](internal/rules/) + [internal/analysis/detectors/](internal/analysis/detectors/))

Detection is **YAML-driven**. The `internal/analysis/detectors` package now
owns only the `Detector` interface and the `Registry` runtime; concrete
detectors are produced by `internal/rules` from embedded YAML policy files.

```go
type Detector interface {
    RuleID() string
    Category() models.DetectorCategory
    Applies(tool models.ToolDef) bool
    Detect(tool models.ToolDef, pf analysis.ParsedFile) []models.Finding
    Singleton() bool
}
```

Pipeline at startup:

1. `rules.DefaultFS()` returns the `embed.FS`-backed filesystem rooted at
   `internal/rules/policies/`.
2. `rules.LoadRegistry(fsys)` walks recursively, decodes every `.yaml` file,
   validates required fields / enums / cross-file rule-ID uniqueness, then
   wraps each `RuleDef` in a `RuleDetector`.
3. `RuleDetector.Detect` evaluates the rule's `MatchExpr` against the tool;
   on a match it emits one `Finding` populated from the rule's metadata.

Discipline: rule evaluation is pure (no I/O, no clocks); predicates may walk
the AST. Every `Finding` MUST carry an `Explanation`, `SuggestedFix`, and
`Confidence` — the YAML schema requires those fields, so the loader rejects a
rule that omits them.

The `Registry` ([detector.go](internal/analysis/detectors/detector.go))
supports `Subset(...categories)` for `--detectors` filtering and runs
detectors in stable order (detector-stable then tool-stable) so output is
reproducible. `Singleton` detectors fire at most once per scan — used by
manifest-level checks like `OSH-004` (no resource limits configured) that
have no per-tool variation.

Shipped rules (one row per YAML rule entry):

| Rule     | Category   | Severity | Source file                             | Notes                                                  |
| -------- | ---------- | -------- | --------------------------------------- | ------------------------------------------------------ |
| CSDK-001 | claude_sdk | low      | `claude_sdk/tool_definition.yaml`       | Missing docstring / description                        |
| CSDK-002 | claude_sdk | medium   | `claude_sdk/tool_definition.yaml`       | Untyped parameters                                     |
| CSDK-003 | claude_sdk | high     | `claude_sdk/network.yaml`               | HTTP call without `timeout=` kwarg                     |
| CSDK-004 | claude_sdk | high     | `claude_sdk/path_safety.yaml`           | Path-like param flows to I/O without per-param `.resolve()`/`realpath()` |
| CSDK-005 | claude_sdk | medium   | `claude_sdk/error_handling.yaml`        | Raises with no try/except wrapping                     |
| CSDK-006 | claude_sdk | medium   | `claude_sdk/idempotency.yaml`           | Mutating verb in name + no idempotency-key param       |
| CSDK-007 | claude_sdk | low      | `claude_sdk/tool_definition.yaml`       | Ambiguous name (`process`, `handle`, `run`, …)         |
| OSH-001  | openshell  | critical | `openshell/shell.yaml`                  | `subprocess(..., shell=True)`                          |
| OSH-002  | openshell  | high     | `openshell/shell.yaml`                  | Shell call without `ALLOWED_COMMANDS` allowlist        |
| OSH-003  | openshell  | high     | `openshell/filesystem.yaml`             | `open(..., "w")` / `shutil.move`/`rmtree` etc.         |
| OSH-004  | openshell  | medium   | `openshell/resources.yaml`              | Singleton: no resource limits configured anywhere      |
| OSH-005  | openshell  | high     | `openshell/network.yaml`                | HTTP call with dynamic URL + no host allowlist         |

### Stage 5 — Scoring ([internal/analysis/scoring.go](internal/analysis/scoring.go))

Per-tool:

```
weighted = Σ severityWeight(finding) * finding.confidence
score    = max(0, 1 - weighted / saturation)        # saturation = 3.0
```

Overall score is the **min** across per-tool scores. The agent is as reliable
as its weakest surface; mean is misleading because one terrible tool and one
perfect tool would average to a "moderate" overall score that hides the
critical exposure.

Both `saturation` and the severity weights in [models.SeverityWeight](internal/models/models.go)
are placeholders pending a corpus eval (architecture § 8). They live in one
place so the curve can be tuned without touching detectors.

### Stage 6 — Generation ([internal/generation/](internal/generation/))

Two generators, both deterministic by contract. Same findings → byte-identical
output. The smoke test in [scanner_test.go](internal/scanner/scanner_test.go)
runs the full pipeline twice on the sample agent and asserts artifact
byte-equality; this guards the contract from regressions.

- **Hooks** ([hooks.go](internal/generation/hooks.go)) — emits
  `hooks/pretooluse_validate.py` (per-tool validators behind a dispatch table)
  and `hooks/posttooluse_log.py` (structured logging stub). A finding is
  hook-eligible if its `FixHints["hook"]` is non-empty, OR if its rule ID is
  one of `CSDK-003` / `CSDK-004` / `CSDK-006` (the rules whose remediation is
  a runtime mutation rather than a code change). `stanzaForFinding` maps each
  hook-eligible rule to the Python lines it injects.
- **Policy** ([policy.go](internal/generation/policy.go)) — emits
  `openshell/policy.yaml`. With no OSH findings the generator still produces a
  defaults-only policy so the user has a starter file. Each OSH rule maps to a
  policy field: OSH-001 → globalDeny; OSH-002 → per-tool commands.allowed;
  OSH-003 → per-tool filesystem.writePrefixes; OSH-005 → per-tool
  network.allowedHosts. Placeholders are emitted as `# TODO:` comments so
  users see what to fill in, rather than the generator inventing plausible
  values.

The OpenShell schema (`apiVersion: openshell.nvidia.com/v1`, `kind:
SandboxPolicy`) is the generator's interpretation pending the real spec link;
renames are mechanical when the spec lands.

### Stage 7 — Review ([internal/review/](internal/review/))

- `Renderer.Render` ([diff.go](internal/review/diff.go)) — produces the human
  scan summary printed to stdout for `--format human`. Color via lipgloss,
  disabled with `--no-color`.
- `ApplyArtifacts` — writes generated files into the repo root. Refuses to
  overwrite by default; `--overwrite` opts in.
- `ExportZIP` ([export.go](internal/review/export.go)) — packages all
  generated artifacts into one ZIP for `--export <path>`.

The CLI's per-finding interactive accept/reject UX from the design doc is out
of scope; `--apply --yes` is the CLI equivalent of "accept all".

---

## 3. Data model

All cross-package values live in [internal/models/](internal/models/). Anything
that crosses ingestion → analysis → generation → review is a typed struct with
JSON tags, because `ScanResult` is the contract for `--format json` CI output.

```go
ScanResult {
    ScanID             string             // sha256(repo + sorted python file list)[:16]
    Repo               string
    Manifest           ScanManifest       // what the normalizer found
    Tools              []ToolDef          // discovery output
    Findings           []Finding          // detector output
    Readiness          []ToolReadiness    // per-tool scores
    OverallScore       float64            // min across tools
    GeneratedArtifacts []GeneratedArtifact
}

ScanManifest {
    RepoRoot, IsRemote, RemoteURL string
    PythonFiles, TypeScriptFiles, JavaScriptFiles []string
    YAMLFiles, JSONFiles, MarkdownFiles []string
    HasClaudeSDKDependency, HasOpenShellArtifact bool
    Components []AgentComponent     // discovered non-tool agent artifacts
}

ToolDef {
    Name           string
    Kind           ToolKind          // claude_sdk_tool | mcp_tool | shell_invocation | unknown
    Language       Language          // python | typescript | javascript | go
    FilePath, Line, EndLine ...
    Description    string
    HasTypedParams bool
    ParamNames     []string
    Facts          map[string]string // detector-injected hints
}

AgentComponent {
    Kind     ComponentKind  // mcp_config | claude_md | claude_settings | subagent | ...
    Path     string         // forward-slash relative to repo root
    Language Language       // set for code components, empty for configs/prompts
    Note     string         // optional human-readable hint
}
```

`ScanID` is derived deterministically from the repo label and the sorted
Python file list, so identical inputs produce diff-comparable JSON across runs.

Discipline rules:

- `RawSource` was deliberately **not** included on `ToolDef`. Carrying full
  function bodies in memory and then in JSON is wasteful, and the LLM
  enrichment path that would consume them is not yet wired.
- `Facts map[string]string` on `ToolDef` is reserved for detector-injected
  hints (e.g., "this function shells out") that downstream stages can read
  without re-walking the AST.
- `Finding.FixHints map[string]any` carries generator-specific keys (e.g.,
  `"hook": "pretooluse_validate"`, `"policy_emit": "command_allowlist"`,
  `"unsafe_params": [...]`). The generators read these instead of hardcoding
  rule IDs in their own logic.
- `AgentComponent.Path` always uses forward slashes (`filepath.ToSlash`),
  even on Windows. This keeps manifest output platform-stable so JSON
  consumers and snapshot tests don't see `/` vs `\` differences.
- `Components` is sorted by `(Kind, Path)` for byte-stable JSON output.

---

## 4. Package layout

```
cmd/karenctl/                    CLI entry point (cobra). main.go only.
internal/
├── models/                      Cross-boundary types. JSON-tagged. Zero deps.
├── ingestion/                   Importer + Normalizer.
├── analysis/
│   ├── astutil/                 Tiny tree-sitter ergonomic layer (NodeText,
│   │                            Walk, FindAll, FunctionName, FunctionParams,
│   │                            FunctionDocstring, FunctionHasTypedParams,
│   │                            HasKwarg).
│   ├── discovery.go             Tool discovery passes.
│   ├── heuristics.go            Domain helpers shared by every detector path:
│   │                            FindFunctionNode, IsHTTPCall, IsPathishParam.
│   ├── scoring.go               Per-tool + overall scoring.
│   └── detectors/               Detector interface + Registry runtime only.
│       └── detector.go          Detector iface, Registry, New(ds), Subset, Run.
├── rules/                       YAML-driven detection engine. Authoritative.
│   ├── schema.go                PolicyFile / RuleDef / MatchExpr types.
│   ├── loader.go                Validating YAML loader (recursive walk).
│   ├── predicates.go            One Pred* per detection primitive.
│   ├── evaluator.go             MatchExpr.Evaluate — recursive walker.
│   ├── rule_detector.go         RuleDetector adapter + LoadRegistry.
│   ├── embed.go                 //go:embed all:policies → DefaultFS().
│   └── policies/                Embedded YAML rule definitions.
│       ├── claude_sdk/
│       │   ├── tool_definition.yaml   CSDK-001, CSDK-002, CSDK-007
│       │   ├── network.yaml           CSDK-003
│       │   ├── path_safety.yaml       CSDK-004
│       │   ├── error_handling.yaml    CSDK-005
│       │   └── idempotency.yaml       CSDK-006
│       └── openshell/
│           ├── shell.yaml             OSH-001, OSH-002
│           ├── filesystem.yaml        OSH-003
│           ├── resources.yaml         OSH-004
│           └── network.yaml           OSH-005
├── generation/                  Hooks + Policy generators (deterministic).
├── review/                      Human renderer, apply, export ZIP.
└── inference/                   BYOK inference router (stub; cache only).
```

### `internal/analysis/heuristics.go` — the shared-helper boundary

Domain-level utilities the rules package and any future Go-native detector
need:

- `FindFunctionNode(t, pf)` — relocate a tool's `function_definition` node.
- `IsHTTPCall(callee)` — exact-text match against the known HTTP client API
  surface. **Limitation**: aliased session calls (`s = requests.Session();
  s.get(...)`) are not resolved. Documented; address with an aliasing pass
  when corpus signal demands it.
- `IsPathishParam(name)` — word-boundary check against path/file/dir names so
  `editor_id` does not match `_dir`.

`HasKwarg(call, src, name)` is in `astutil` because it is pure tree-sitter and
has no domain knowledge.

---

## 5. The rules engine: schema, evaluator, embed

YAML rule files live under `internal/rules/policies/`, grouped first by
detector category and then by topic. Each file is a single `policy:` block
with one or more rules:

```yaml
policy:
  id: claude_sdk_network
  name: Network call hygiene
  category: claude_sdk
  description: >
    Rules covering outbound network calls made from inside agent tools.
rules:
  - id: CSDK-003
    title: Network call has no timeout
    severity: high
    confidence: 0.85
    applies_to: [claude_sdk_tool, mcp_tool]
    singleton: false
    match:
      call_without_kwarg:
        callees: [requests.get, requests.post, ...]
        missing: timeout
    explanation: >
      An agent tool that makes a network request without a timeout ...
    fix: Pass `timeout=` (typically 5–30s) to the request.
    fix_hints:
      hook: pretooluse_validate
      guard: timeout_required
```

### Adding a rule

1. Pick the right category subdirectory (`claude_sdk/` or `openshell/`).
2. Either append to an existing topic file or create a new `<topic>.yaml`
   file — the loader walks recursively so new files are picked up
   automatically by `go:embed`.
3. Use a fresh rule ID that does not collide with any existing ID across all
   policy files (the loader rejects duplicates).
4. If your rule needs a primitive the schema does not yet expose, extend
   `MatchExpr` in `schema.go` and add a corresponding `Pred*` function in
   `predicates.go`. The evaluator wires them together by name.

### Evaluator semantics ([evaluator.go](internal/rules/evaluator.go))

`MatchExpr.Evaluate` is a recursive conjunctive walker:

- An empty `MatchExpr` returns true (vacuously matches; useful for singleton
  rules with no predicate body).
- Every set field on a node contributes one boolean to a logical AND: all
  combinators, all primitives, and all nested struct predicates that are
  non-nil must hold for the node to match.
- The combinators `all` and `any` recurse; `not` negates the whole subtree.

The conjunctive default makes simple one-predicate rules read naturally as
YAML, while combinators are available when a rule needs disjunction or
negation.

### Loader contract ([loader.go](internal/rules/loader.go))

- `fs.WalkDir` from the FS root, picking up every `*.yaml` recursively.
- File handles are closed inside the per-file decode helper, not deferred to
  Load's return — so the descriptor budget is bounded by iteration, not by
  total policy count.
- `KnownFields(true)` rejects unknown YAML keys (catches typos like
  `has_blah` immediately).
- Required-field validation, enum validation (severity / category),
  duplicate-rule-ID detection across files. Every error is collected via
  `errors.Join` so a contributor sees every problem in one run.

### Embedding ([embed.go](internal/rules/embed.go))

`//go:embed all:policies` bundles the YAML files into the binary at compile
time. `DefaultFS()` returns an `fs.FS` rooted at the policies directory (the
`policies/` prefix is stripped via `fs.Sub`), so the loader sees paths like
`claude_sdk/network.yaml`.

This is what `scanner.Run` uses by default. Tests can pass an `fstest.MapFS`
or any other `fs.FS` to `LoadRegistry` to exercise alternate rule sets.

### Detector interface boundary ([detectors/detector.go](internal/analysis/detectors/detector.go))

The `detectors` package is now interface + runtime only. It owns:

- The `Detector` interface (RuleID, Category, Applies, Detect, Singleton).
- The `Registry` type with `New(ds []Detector)`, `Subset(cats...)`, `Run`,
  and `Count`.

It deliberately ships no concrete detectors. Any producer (the rules engine
today; potentially a future Go-native or LLM-judged producer) builds its own
`[]Detector` and hands it to `detectors.New(...)`. This keeps the runtime
agnostic to where a rule comes from.

---

## 6. Determinism contract

Two invariants are load-bearing for the user-facing experience and are tested:

1. **Same inputs → same `ScanID`.** Derived from a sorted file list, so file
   ordering from the OS walk does not leak into the ID.
2. **Same findings → byte-identical generated artifacts.** Generators sort
   tool names, sort findings by `RuleID`, and dedupe global deny entries by
   `(RuleID, Reason)` before marshaling. `TestScanSampleAgent` runs the full
   pipeline twice and asserts equality.

This matters because users commit the generated files. A non-deterministic
generator means a user sees spurious diffs on every CI run, which trains them
to ignore the diff entirely.

---

## 7. CLI surface ([cmd/karenctl/main.go](cmd/karenctl/main.go))

```
karenctl scan <target> [--detectors=…] [--format=human|json]
                       [--apply [--yes] [--overwrite]]
                       [--export=path.zip]
                       [--strict] [--no-color]
karenctl version
```

Exit codes:

- `0` — no findings ≥ medium (or no findings at all).
- `1` — at least one finding ≥ medium, OR `--strict` and any finding present.
- `2` — scanner / I/O error.

The CLI is a thin shell over `scanner.Run`. The same `Run(Config) (ScanResult,
error)` is what a future HTTP server, GitHub Action, or test harness calls;
the boundary is intentionally narrow.

---

## 8. Build constraint: CGO

tree-sitter is a C library, so `CGO_ENABLED=1` is required. `README.md`
documents zig-cc as the easiest cross-compile path. If single-binary,
no-CGO distribution becomes a hard requirement, the swap target is
`github.com/go-python/gpython` with reduced fidelity on modern Python
(walrus, structural pattern matching, etc.). This is a known cliff; do not
take it absent a concrete distribution requirement.

---

## 9. What is intentionally out

- **No LLM enrichment yet.** `internal/inference/router.go` defines the BYOK
  interface and an in-process cache; `Call()` returns `ErrLLMDisabled` when
  no API key is set. The planned first target is upgrading low-confidence
  rule-based hits to confirmed findings (CSDK-005 raw-exception detection is
  the highest-leverage rule for this).
- **No corpus-eval benchmark.** The architecture doc § 8 calls for 20–40
  real agent repos before MVP is "done". This skeleton ships rule-based
  detectors and a single-fixture smoke test; that is not the corpus eval.
- **No web app, no API server, no GitHub Action.** CLI-only.
- **Per-finding interactive accept/edit/reject.** `--apply --yes` is the CLI
  equivalent of accept-all; richer UX waits for a host that can render it.
