# karenctl detection policies

Every `.yaml` file under this directory defines one or more detection rules.
The contents are embedded into the binary at build time
(`//go:embed all:policies` in [`../embed.go`](../embed.go)) and loaded at
scanner startup.

## Layout

Rules are grouped by `<category>/<topic>.yaml`:

```
policies/
├── claude_sdk/                      Claude Agent SDK reliability rules (CSDK-NNN)
│   ├── tool_definition.yaml
│   ├── network.yaml
│   ├── path_safety.yaml
│   ├── error_handling.yaml
│   └── idempotency.yaml
└── openshell/                       OpenShell sandbox policy rules (OSH-NNN)
    ├── shell.yaml
    ├── filesystem.yaml
    ├── resources.yaml
    └── network.yaml
```

The category is the first path segment. The topic file is your call —
group related rules. One rule per file is overkill; fifty rules per file is
unwieldy. Topic files of 1-5 rules read best.

The loader walks recursively, so a new category like `openai/` or `mcp/`
just works once you drop a YAML in it.

## Schema

Every accepted field is documented with annotations in
[`../schema.yaml`](../schema.yaml). That file is the authoritative reference
— read it before authoring a new rule.

## Adding a rule

1. Pick the right category subdirectory. If your rule is for a new category,
   create the directory and put the YAML there.
2. Either append to an existing topic file or create a new `<topic>.yaml`.
3. Fill in every required field (`id`, `title`, `severity`, `confidence`,
   `applies_to`, `match`, `explanation`, `fix`). The loader rejects the
   file on omission.
4. Pick a fresh rule ID using the `<CATEGORY-NNN>` convention. The loader
   rejects duplicates across all policy files.
5. Add a triggering Python function to
   [`examples/sample_agent/tools.py`](../../../examples/sample_agent/tools.py)
   and add the rule ID to `expectedRules` in
   [`internal/scanner/scanner_test.go`](../../scanner/scanner_test.go).
6. Run `go test ./...` from the repo root.

## When you need a primitive that does not exist yet

The schema is closed (the loader uses `KnownFields(true)`). Adding a new
predicate is a four-file change:

1. Add the field to `MatchExpr` in [`../schema.go`](../schema.go).
2. Add a `Pred*` function in [`../predicates.go`](../predicates.go) and a
   test case in [`../predicates_test.go`](../predicates_test.go).
3. Wire it into [`../evaluator.go`](../evaluator.go) — one extra
   `if e.X != nil && !PredX(...)` clause.
4. Document the field in [`../schema.yaml`](../schema.yaml).

Make all four changes in the same commit. Skipping any one of them creates
a silent gap.

## Loader behavior

- Recursive walk via `fs.WalkDir`. Every `.yaml` file is loaded.
- `KnownFields(true)` — typos in field names fail the load.
- Errors are batched via `errors.Join` so you see every problem in one run.
- Cross-file rule-ID uniqueness is enforced with a "previously defined in
  X" message.

## Language scope

Each rule declares a `language:` field that gates which tools it can fire
against. Today the only language with a tool-discovery parser plumbed in is
**Python**, so:

- New Python rules can omit `language:` and the loader fills in `python`.
  Existing rules state it explicitly anyway, as good documentation practice.
- TypeScript / JavaScript / Go rules MUST declare `language: typescript`
  (etc.) explicitly. They will load and validate but stay inert until the
  corresponding parser is plumbed into `internal/analysis/discovery.go` and
  `internal/analysis/astutil/`.

Allowed values: `python | typescript | javascript | go`. The loader rejects
anything else.

## Severity guidance

| Severity | Use for                                                            |
| -------- | ------------------------------------------------------------------ |
| critical | Active exploitability vector with no in-band defense (e.g. shell=True). |
| high     | Likely-exploitable or non-recoverable in production.               |
| medium   | Reliability gap; not a security issue but causes failures.         |
| low      | Quality / clarity issue; degrades agent-tool selection.            |
| info     | Reserved; do not ship rules at this level for now.                 |

## Confidence guidance

`confidence` is your estimate of how often this rule's match is the *real*
problem versus a false positive. Calibrated against the sample fixture:

- `>= 0.9` — high-precision pattern (e.g. `shell=True` is unambiguous).
- `0.7–0.9` — heuristic with known false-positive shapes.
- `0.5–0.7` — exploratory; expect noise.
- `< 0.5` — do not ship; reconsider the predicate.

Confidence multiplies severity weight in the per-tool score, so low-
confidence rules contribute proportionally less to readiness.

## Determinism

Generated artifacts are byte-equal across repeat scans of the same input.
That property is a load-bearing contract (users commit the generated files;
spurious diffs train them to ignore the diff). Two consequences for rule
authoring:

- Don't write predicates that depend on map iteration order.
- The `fix_hints` map is sorted on serialization; safe to use freely.

The smoke test
[`internal/scanner/scanner_test.go`](../../scanner/scanner_test.go) runs
two full scans and asserts artifact byte-equality. Don't disable it.
