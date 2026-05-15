# karenctl

> Temporary name. See note in the project tracker about renaming before this leaks
> into commit history and screenshots.

Static analyzer for agent reliability. Scans a Claude Agent SDK repo, finds reliability
weaknesses, emits committable artifacts (Pre/PostToolUse hook configs +
NVIDIA OpenShell sandbox policies).

Implements the Phase 1 MVP scope of *Trustabl Architecture v1 (Strawman)* as a single
Go binary.

## Status

Skeleton. Critical path is wired end-to-end. Detection runs from YAML rule
files embedded at build time via `go:embed`; see
[ARCHITECTURE.md](ARCHITECTURE.md) for the engine and `internal/rules/policies/`
for the rule definitions.

**Language scope.** Tool discovery is **Python-only** today —
TypeScript / JavaScript / Go files are recognized in the file inventory and
contribute to agent-component discovery (MCP configs, hooks, manifests,
etc.), but no AST parser for those languages is plumbed in, so no tools are
extracted from them. The rule schema's `language:` field is in place for
multi-language rule sets when those parsers ship. See
[ARCHITECTURE.md § 1.1](ARCHITECTURE.md#11-language-scope).

The following are intentionally stubbed and called out where they live:

- **LLM enrichment** (`internal/inference/router.go`) — typed BYOK interface, no
  Anthropic call yet. Rule-based detectors run without it.
- **Confidence scores** — heuristic, not LLM-judged.
- **Detection-quality benchmark** — no corpus eval (§8 of the architecture doc says
  you need 20–40 real agent repos before MVP is "done"; this is not that).
- **No web app, no API server, no GitHub Action.** This is the CLI surface only.

## Build

CGO is required because the Python AST parser uses tree-sitter:

```bash
# macOS / Linux
CGO_ENABLED=1 go build -o karenctl ./cmd/karenctl

# Cross-compile: pick a C toolchain for the target. zig is the easiest.
CGO_ENABLED=1 CC="zig cc -target x86_64-linux-gnu" \
  GOOS=linux GOARCH=amd64 go build -o karenctl-linux ./cmd/karenctl
```

This is the cost of using tree-sitter for accurate Python parsing. If single-binary,
no-CGO distribution becomes a hard requirement later, swap the parser for
`github.com/go-python/gpython` and accept lower fidelity on modern Python.

## Use

```bash
# Local repo
karenctl scan ./path/to/agent-repo

# GitHub repo (shallow clone to temp dir, removed on exit)
karenctl scan https://github.com/org/repo

# Restrict detectors
karenctl scan ./repo --detectors claude_sdk
karenctl scan ./repo --detectors openshell
karenctl scan ./repo --detectors claude_sdk,openshell

# Apply generated artifacts (writes hooks/ and openshell/ into the repo;
# requires --yes or interactive approval)
karenctl scan ./repo --apply --yes

# Export the bundle as a ZIP
karenctl scan ./repo --export bundle.zip

# JSON output (for CI piping)
karenctl scan ./repo --format json
```

Exit codes: `0` = no findings ≥ medium, `1` = findings ≥ medium present, `2` =
scanner error. Comment-only mode in CI never blocks — that's a paid-tier feature
per architecture §6.

## Produced artifacts

Per §4 of the architecture, the artifacts get committed to the user's repo:

```
<repo>/
├── hooks/
│   ├── pretooluse_validate.py
│   └── posttooluse_log.py
├── openshell/
│   └── policy.yaml
└── otel/
    └── trace_config.yaml          # deferred (Phase 2) — not generated
```

## Layout

| Architecture node | Code path                                |
| ----------------- | ---------------------------------------- |
| Importer          | `internal/ingestion/importer.go`         |
| Normalizer        | `internal/ingestion/normalizer.go`       |
| Tool Discovery    | `internal/analysis/discovery.go`         |
| Detector runtime  | `internal/analysis/detectors/`           |
| Detector rules    | `internal/rules/policies/<category>/`    |
| Rule engine       | `internal/rules/{schema,loader,evaluator,predicates,rule_detector,embed}.go` |
| Scoring Engine    | `internal/analysis/scoring.go`           |
| Hook Generator    | `internal/generation/hooks.go`           |
| Policy Generator  | `internal/generation/policy.go`          |
| Diff Renderer     | `internal/review/diff.go`                |
| Exporter          | `internal/review/export.go`              |
| Inference Router  | `internal/inference/router.go` (stub)    |

## Detectors shipped in this skeleton

Naming: `CSDK-NNN` for Claude SDK reliability, `OSH-NNN` for OpenShell policy.
Rules are defined as YAML in `internal/rules/policies/<category>/<topic>.yaml`
and embedded into the binary via `go:embed`. To add a rule, drop a new YAML
entry; no Go code change is required unless the rule needs a new predicate
primitive (see [ARCHITECTURE.md § 5](ARCHITECTURE.md#5-the-rules-engine-schema-evaluator-embed)).

| Rule     | Title                                              | Severity | Source file                       |
| -------- | -------------------------------------------------- | -------- | --------------------------------- |
| CSDK-001 | Tool function has no docstring / description       | low      | `claude_sdk/tool_definition.yaml` |
| CSDK-002 | Tool function has no type-annotated params         | medium   | `claude_sdk/tool_definition.yaml` |
| CSDK-003 | Tool performs network I/O without timeout          | high     | `claude_sdk/network.yaml`         |
| CSDK-004 | Tool accepts user-supplied path without validation | high     | `claude_sdk/path_safety.yaml`     |
| CSDK-005 | Tool raises raw exceptions (no error contract)     | medium   | `claude_sdk/error_handling.yaml`  |
| CSDK-006 | Tool with side-effects has no idempotency hint     | medium   | `claude_sdk/idempotency.yaml`     |
| CSDK-007 | Ambiguous tool name (`process`, `handle`, ...)     | low      | `claude_sdk/tool_definition.yaml` |
| OSH-001  | `subprocess` call with `shell=True`                | critical | `openshell/shell.yaml`            |
| OSH-002  | Shell tool without allowed-command list            | high     | `openshell/shell.yaml`            |
| OSH-003  | Filesystem write without path restriction          | high     | `openshell/filesystem.yaml`       |
| OSH-004  | No resource limits configured                      | medium   | `openshell/resources.yaml`        |
| OSH-005  | Broad network egress (no host allowlist)           | high     | `openshell/network.yaml`          |

This is 12 detectors against the architecture's "15 reliability checks" headline.
Three more are easy adds once corpus data tells you which patterns produce
real findings vs false positives. Resist the urge to ship more rules without
that data.
