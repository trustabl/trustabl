# trustabl detection policies

Every `.yaml` file under this directory defines one or more detection rules.

> **Test mirror — not the production source.** These rule packs are **not**
> embedded in the binary. Production rules live in the external
> `trustabl-rules` git repository (`https://github.com/trustabl/trustabl-rules`)
> and are resolved at scan time (see `internal/rulesource/`). This
> `testdata/rules-fixture/` directory is an **in-engine copy** of those packs,
> injected via `os.DirFS` so `go test` can validate rules offline. **It must be
> kept in sync with the live rules repo** — see the "Two-repo rule model"
> section in the top-level [`CLAUDE.md`](../../CLAUDE.md). The `../` relative
> links below refer to the engine's `internal/rules/` package — from this
> fixture location that is `../../internal/rules/`.

The loader walks this tree at scan time (skipping the top-level
`manifest.yaml`, which declares the pack's `schema_version`).

## Layout

Rules are grouped by `<category>/<topic>.yaml`:

```
policies/
├── claude_sdk/                      Claude Agent SDK rules (CSDK-NNN)
│   ├── agent_safety.yaml            CSDK-101, CSDK-102 (agent scope)
│   ├── error_handling.yaml          CSDK-005
│   ├── idempotency.yaml             CSDK-006
│   ├── network.yaml                 CSDK-003
│   ├── path_safety.yaml             CSDK-004
│   ├── subagent_safety.yaml         CSDK-110 (subagent scope)
│   └── tool_definition.yaml         CSDK-001, CSDK-002, CSDK-007
├── google_adk/                      Google ADK rules (ADK-NNN)
│   ├── agent_safety.yaml            ADK-101..105 (agent scope)
│   ├── builtin_tools.yaml           ADK-008
│   ├── error_handling.yaml          ADK-005
│   ├── idempotency.yaml             ADK-006
│   ├── network.yaml                 ADK-003
│   ├── path_safety.yaml             ADK-004
│   └── tool_definition.yaml         ADK-001, ADK-002, ADK-007
└── openai_sdk/                      OpenAI Agents SDK rules (OAI-NNN)
    ├── agent_safety.yaml            OAI-101..104, OAI-109 (agent scope)
    ├── code_execution.yaml          OAI-013
    ├── decorator_config.yaml        OAI-003, OAI-004
    ├── error_handling.yaml          OAI-008
    ├── idempotency.yaml             OAI-009
    ├── mcp_safety.yaml              OAI-106 (agent scope)
    ├── network.yaml                 OAI-005, OAI-011
    ├── observability.yaml           OAI-010
    ├── path_safety.yaml             OAI-006
    ├── shell_safety.yaml            OAI-012
    ├── tool_definition.yaml         OAI-001, OAI-002, OAI-007
    └── tracing.yaml                 OAI-201 (repo scope)

# Note: an openshell/ subdirectory previously held OSH-001..005 (NVIDIA
# OpenShell sandbox rules). That pack moved to a closed-source companion
# project. Don't author new OSH rules here — they belong in that project.
```

The category is the first path segment. The topic file is your call —
group related rules. One rule per file is overkill; fifty rules per file is
unwieldy. Topic files of 1-5 rules read best.

The loader walks recursively, so a new category just works once you drop a
YAML in it. To add a new SDK (e.g. `mcp/`), create the directory and use
the matching `category:` value (extend `models.DetectorCategory` and the
loader's category-enum switch first if it's not yet recognized).

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
5. Add at least one fire case and one silent case for the new rule to
   `policyRuleCases` in [`../policies_test.go`](../policies_test.go). The
   `TestPolicyRules_AllRulesCovered` guard fails at build time if a shipped
   rule has no test coverage — this is contract, not best practice.
6. Run `go test ./...` from the repo root.

When the goal is raising rule *quality* rather than adding a rule, see
**Path to production-grade (known gaps)** in [`CLAUDE.md`](CLAUDE.md) — it
tracks the predicate and test-harness work (value-aware timeout checks,
Session/Client alias matching, hosted/decorated shell-tool coverage, and
source-level fire/silent fixtures) needed to move the pack from "signal to
investigate" toward an authoritative gate.

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

## SDK scope per rule

A rule declares which tool kinds it applies to via `applies_to`. The shipped
kinds are:

| Kind               | Discovered when                                                  |
| ------------------ | ---------------------------------------------------------------- |
| `claude_sdk_tool`  | Function decorated with `@tool` / `@claude_tool` / `claude_agent_sdk` (substring) |
| `openai_tool`      | Function decorated with `@function_tool` (OpenAI Agents SDK)     |
| `mcp_tool`         | Function decorated with `@server.tool` / `@mcp.tool` / `.register_tool` |
| `shell_invocation` | Bare function whose body calls `subprocess.*` / `os.system` / `os.popen` (no rules currently target this — OSH-* moved to a closed-source project) |
| `unknown`          | Fallback — rarely useful in `applies_to`                         |

**Be honest about scope.** It is tempting to add every kind to `applies_to`
because the AST pattern is the same — an HTTP call without `timeout=` is
the same shape regardless of which SDK calls it. Resist this. A rule's
`explanation` and `fix` text usually references a specific SDK ("the Claude
Agent SDK uses the docstring as the description shown to the model").
Listing a kind whose SDK doesn't match makes the user-facing text lie.

If a pattern truly applies cross-SDK, author one rule per SDK with the
framing each SDK requires (different `explanation` and `fix` wording).
Duplication of the predicate is the price of honest framing.

The shipped `policies/claude_sdk/` and `policies/openai_sdk/` packs follow
this discipline — the structurally-similar "missing docstring" rule appears
as CSDK-001 and OAI-001 with SDK-specific framing in each, not as one
rule with `applies_to: [claude_sdk_tool, openai_tool]`.

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
| info     | Reserved; do not ship rules at this level.                         |

## Confidence guidance

`confidence` is your estimate of how often this rule's match is the *real*
problem versus a false positive. Calibration is by author judgement until
a corpus eval lands:

- `>= 0.9` — high-precision pattern (e.g. `shell=True` is unambiguous).
- `0.7–0.9` — heuristic with known false-positive shapes.
- `0.5–0.7` — exploratory; expect noise.
- `< 0.5` — do not ship; reconsider the predicate.

Confidence multiplies severity weight in the per-tool score, so low-
confidence rules contribute proportionally less to readiness.

## Determinism

The scan output is byte-stable across repeat scans of the same input. That
property is a load-bearing contract (CI consumers diff scan output; spurious
diffs train them to ignore it). One consequence for rule authoring:

- Don't write predicates that depend on map iteration order.

This is enforced by [`internal/scanner/determinism_test.go`](../../scanner/determinism_test.go),
which runs `scanner.Run` twice over `testdata/deterministic-fixture` and
asserts that `ScanID` is identical across both runs. A non-deterministic rule
is a build failure, not a latent bug.
