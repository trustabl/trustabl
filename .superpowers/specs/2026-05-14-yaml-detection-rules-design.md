# Detection Rules: YAML Policy Files + Parallel Execution

**Date:** 2026-05-14
**Status:** Approved

## Problem

Detection rules are currently hardcoded Go structs in `internal/analysis/detectors/`. Adding or modifying a rule requires writing Go. The goal is to make rules authorable by domain contributors (security analysts, agent developers, open-source contributors) without touching Go code.

## Goals

- Rules and policies defined in YAML files under `rules/`
- All detection logic expressible declaratively via a predicate vocabulary
- Detectors run in parallel
- Existing end-to-end test passes without modification after migration
- Contributors add rules by writing YAML, not Go

## Non-Goals

- Rego / OPA integration (dropped)
- Scanning IaC or config files
- Runtime rule reloading (rules load once at startup)

---

## File Layout

```
internal/rules/
  policies/
    claude_sdk.yaml      # CSDK-001 through CSDK-007
    openshell.yaml       # OSH-001 through OSH-005
  embed.go               # //go:embed policies/*.yaml → DefaultFS fs.FS
  ...
```

YAML files live under `internal/rules/policies/` so they can be embedded via `go:embed` from within the same package. `embed.go` exports `DefaultFS` as an `fs.FS`. The loader accepts an `fs.FS` parameter — tests pass an `os.DirFS` or an in-memory filesystem; the binary uses `DefaultFS`. Future policies are additional `.yaml` files in `policies/`.

`Config.RulesDir string` in `scanner.Config` overrides the default: when non-empty, the scanner loads rules from disk at that path instead of the embedded FS. This is how organisations ship custom rule sets without recompiling.

---

## YAML Schema

### Policy file

```yaml
policy:
  id: claude_sdk                          # stable identifier, matches category
  name: Claude Agent SDK Reliability      # human label
  category: claude_sdk                    # maps to models.DetectorCategory
  description: |
    Reliability checks for tool functions decorated with the Claude Agent SDK.

rules:
  - ...
```

### Rule

```yaml
- id: CSDK-001                            # stable rule ID
  title: Tool has no description          # one-line summary
  severity: low                           # info | low | medium | high | critical
  confidence: 0.95                        # 0..1
  applies_to:                             # tool kinds this rule runs against
    - claude_sdk_tool
    - mcp_tool
  singleton: false                        # true = fire at most once per scan
  match:                                  # detection logic (see Predicate Vocabulary)
    not:
      has_docstring: true
  explanation: |
    The Claude Agent SDK uses the tool's docstring as the description shown
    to the model. Without one, the model must guess from the function name
    when to call this tool.
  fix: Add a one-paragraph docstring describing inputs, outputs, and when to use this tool.
  fix_hints:                              # optional; drives hook + policy generation
    add_docstring: true
```

**Required fields:** `id`, `title`, `severity`, `confidence`, `applies_to`, `match`, `explanation`, `fix`

**Optional fields:** `singleton` (default false), `fix_hints`

**Validation at load time:** unknown predicate names are a hard error; missing required fields are a hard error; duplicate rule IDs across all loaded files are a hard error.

---

## Predicate Vocabulary

### Combinators

```yaml
all: [...]        # every condition must pass (AND)
any: [...]        # at least one condition must pass (OR)
not: {...}        # negate one condition
```

### Tool metadata predicates

| Predicate | Type | Description |
|-----------|------|-------------|
| `has_docstring` | bool | function has a leading string literal |
| `has_params` | bool | has at least one non-self/cls parameter |
| `has_typed_params` | bool | at least one type-annotated parameter |
| `has_raise` | bool | body contains a raise statement |
| `has_try_except` | bool | body contains a try/except block |
| `has_shell_call` | bool | calls subprocess.* / os.system / os.popen |
| `has_write_call` | bool | calls open(w/a/x) or shutil.copy/move/rmtree |
| `has_dynamic_url_call` | bool | HTTP call whose URL arg is not a string literal |
| `always` | bool | unconditionally matches; use with `singleton: true` |

```yaml
name_in: [process, handle, run]       # tool name is in the set (lowercased)
name_has_prefix: [create_, send_]     # tool name starts with one of these (lowercased)

param_name_matches:                   # any param name satisfies at least one sub-pattern
  exact: [request_id, txn_id]
  contains: [idempot]
  suffixes: [_path, _file, _dir, _directory]
  prefixes: [file_, path_]

has_body_text: [.resolve(, realpath(] # any string appears literally in the function body
```

### Call-site predicates

```yaml
call_without_kwarg:                   # a matching call exists without the named kwarg
  callees: [requests.get, httpx.post]
  missing: timeout

call_with_kwarg_value:                # a matching call has kwarg == value
  callee_prefix: subprocess.
  kwarg: shell
  value: "True"

call_uses_param:                      # a matching call receives a path-like param as an identifier arg
  callees: [open, Path]
  callee_prefixes: [os., shutil.]
```

### Full rule examples

**CSDK-002 — Untyped params**
```yaml
match:
  all:
    - has_params: true
    - not:
        has_typed_params: true
```

**CSDK-004 — Unsafe path**
```yaml
match:
  all:
    - param_name_matches:
        exact: [path, file, filename, filepath, dir, directory]
        suffixes: [_path, _file, _dir, _directory]
        prefixes: [file_, path_]
    - call_uses_param:
        callees: [open, Path]
        callee_prefixes: [os., shutil.]
    - not:
        has_body_text: [.resolve(, realpath(, is_safe_path]
```

**OSH-001 — shell=True**
```yaml
match:
  call_with_kwarg_value:
    callee_prefix: subprocess.
    kwarg: shell
    value: "True"
```

**OSH-004 — No resource limits (singleton)**
```yaml
singleton: true
match:
  always: true
```

---

## Go Architecture

### New package: `internal/rules/`

| File | Responsibility |
|------|---------------|
| `schema.go` | Go structs mirroring the YAML: `PolicyFile`, `RuleDef`, `MatchExpr` |
| `loader.go` | Reads a directory, unmarshals + validates each `.yaml` file, returns `[]PolicyFile` |
| `evaluator.go` | `Evaluate(expr MatchExpr, tool models.ToolDef, pf analysis.ParsedFile) bool` — recursive combinator walker |
| `predicates.go` | One function per predicate, called by evaluator |

`loader.go` rejects files with: missing required fields, unknown predicate names, duplicate rule IDs. All errors are collected and returned together, not fail-fast, so a contributor sees all problems in one run.

### Changed: `internal/analysis/detectors/`

All individual Go detector structs (`csdk001MissingDocstring` through `osh005BroadNetworkEgress`) are deleted. A single `RuleDetector` replaces them:

```go
type RuleDetector struct {
    def rules.RuleDef
}

func (d RuleDetector) RuleID() string                                   { return d.def.ID }
func (d RuleDetector) Category() models.DetectorCategory                { return d.def.Category }
func (d RuleDetector) Singleton() bool                                  { return d.def.Singleton }
func (d RuleDetector) Applies(t models.ToolDef) bool                    { /* check applies_to */ }
func (d RuleDetector) Detect(t models.ToolDef, pf analysis.ParsedFile) []models.Finding {
    if !rules.Evaluate(d.def.Match, t, pf) {
        return nil
    }
    return []models.Finding{{ /* populated from d.def fields */ }}
}
```

`NewRegistry()` is replaced by `LoadRegistry(rulesDir string) (*Registry, error)`.

The exported constructor functions (`CSDK001()` through `OSH005()`) added for unit tests are removed; tests will load from YAML fixtures instead.

The `notSingleton` embed is removed — singleton behavior is now a field on `RuleDef`.

### Changed: `Registry.Run` — parallel execution

A bounded worker pool replaces the sequential nested loop. Pool size defaults to `runtime.GOMAXPROCS(0)`.

```go
type workItem struct {
    detector Detector
    tool     models.ToolDef
    pf       analysis.ParsedFile
}
```

Work items are enqueued to a buffered channel. Workers pull items, call `Detect`, and send findings to a results channel. A `sync.Map` tracks which singleton rule IDs have already fired. The main goroutine collects all findings once the pool drains.

Output order is deterministic: findings are sorted by `(RuleID, FilePath, Line)` after collection.

### Changed: `internal/scanner/scanner.go`

`Config` gains a `RulesDir string` field. Empty string defaults to the embedded `rules/` directory. `Run()` calls `detectors.LoadRegistry(cfg.RulesDir)` instead of `detectors.NewRegistry()`.

### Unchanged

`models`, `ingestion`, `generation`, `review`, `inference` — no changes. The `Detector` interface is unchanged. The `--detectors` flag works because category comes from each policy's `category` field.

---

## Testing

### `internal/rules/predicates_test.go`
One fire case and one silent case per predicate, using inline Python snippets and the `astutil.Parse` helper. No scanner pipeline involved.

### `internal/rules/evaluator_test.go`
Tests combinator logic in isolation using stub predicates: `all` short-circuits on first false, `any` short-circuits on first true, `not` inverts correctly.

### `internal/rules/loader_test.go`
- Valid file loads cleanly
- Missing required field is rejected with a descriptive error
- Unknown predicate name is rejected
- Duplicate rule ID across two files is rejected

### `internal/scanner/scanner_test.go`
Unchanged. Serves as the migration correctness gate: all 12 rules must fire on `examples/sample_agent/tools.py` and both artifact files must be present. If the YAML port is correct, this test passes without modification.

### `internal/rules/policies_test.go` (new)
Loads each YAML policy file from `policies/` via `DefaultFS` and asserts each rule fires on a minimal inline Python fixture. One subtest per rule ID. Catches regressions in individual rule expressions without running the full scanner pipeline.

---

## Migration Path

1. Implement `internal/rules/` (schema, loader, evaluator, predicates)
2. Implement `RuleDetector` and `LoadRegistry`
3. Parallelize `Registry.Run`
4. Write `rules/claude_sdk.yaml` — port CSDK-001 through CSDK-007
5. Write `rules/openshell.yaml` — port OSH-001 through OSH-005
6. Delete Go detector structs
7. All tests pass — scanner end-to-end test is the gate

Each step is independently buildable. The old Go detectors can coexist with `LoadRegistry` during migration — both implement `Detector`.
