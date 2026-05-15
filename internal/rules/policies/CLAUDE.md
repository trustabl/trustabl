# Instructions for Claude — detection policies

These instructions apply to any work inside `internal/rules/policies/`.

## Required reading order before editing

1. [`../schema.yaml`](../schema.yaml) — authoritative field reference.
2. [`README.md`](README.md) — author conventions in this directory.
3. The closest existing rule to what's being asked for — pattern example.

Do not skip step 1.

## Hard rules

- **Never invent YAML keys.** The schema is closed
  (`KnownFields(true)`). If a field you want does not exist in
  [`../schema.go`](../schema.go), extending the schema is a four-file
  change (schema.go + predicates.go + evaluator.go + schema.yaml). Make
  all four changes in one commit.
- **Never change a rule's `id` after it has shipped.** IDs are external
  identifiers; downstream consumers cite them.
- **Never duplicate a rule ID across files.** The loader rejects this at
  startup; the smoke test catches it faster — run `go test ./...`.
- **Never silence the smoke test** in
  [`../../scanner/scanner_test.go`](../../scanner/scanner_test.go) to make a
  rule "work". If the test fails after your edit, fix the rule or fix the
  fixture. Do not delete the assertion.
- **Never write rules at `info` severity.** Reserved.

## Required fields per rule

Every rule MUST set: `id`, `title`, `severity`, `confidence`,
`applies_to`, `match`, `explanation`, `fix`. The loader refuses to start
the scanner if any are missing — this surfaces as a `scan: ...` error in
the CLI.

`language:` is OPTIONAL but state it explicitly — defaults to `python`
when omitted. For TypeScript / JavaScript / Go rules, set it explicitly:
`language: typescript`. Note: only Python tool discovery is plumbed in
today, so non-python rules will load successfully but never fire until
the matching parser ships.

## "Add a rule for X"

Default sequence:

1. Read the structurally-closest existing rule (e.g. for a new network
   rule, read `claude_sdk/network.yaml`).
2. Decide whether existing predicates cover the case. Existing first; new
   primitives are a real cost.
3. Pick the category subdirectory and topic file. If neither matches, ASK
   the user before creating new ones.
4. Write the rule. Match the explanation/fix tone of nearby rules — a
   paragraph, plain language, names the consequence and the fix concretely.
5. Add a triggering function to
   [`../../../examples/sample_agent/tools.py`](../../../examples/sample_agent/tools.py)
   with a comment listing which rules it triggers.
6. Add the new rule ID to `expectedRules` in
   [`../../scanner/scanner_test.go`](../../scanner/scanner_test.go).
7. Run `go test ./...`.

## "Remove a rule"

1. Delete the rule entry from its YAML file. If the file is now empty,
   delete the file.
2. Remove the rule ID from `expectedRules` in
   [`../../scanner/scanner_test.go`](../../scanner/scanner_test.go).
3. Remove the corresponding triggering function from the sample agent IF
   nothing else relies on it.
4. Run `go test ./...`.

## "Change a rule's severity / confidence / explanation"

In-place edit. No fixture changes required. Run `go test ./...` — the
smoke test only asserts that rules fire, not what severity they fire at.

## When the smoke test fails after you add a rule

The smoke test
([`../../scanner/scanner_test.go`](../../scanner/scanner_test.go)) asserts
every rule in `expectedRules` fires at least once on the sample agent.
Two valid responses:

1. The rule SHOULD fire — extend
   [`../../../examples/sample_agent/tools.py`](../../../examples/sample_agent/tools.py)
   with a function that triggers it. Update the comment at the top of that
   function.
2. The rule legitimately doesn't apply to the sample agent (rare; usually
   means the rule targets a category not represented). Document why in a
   comment in `scanner_test.go` next to the omission.

## Output discipline for explanation/fix text

These strings are user-facing — they appear in the CLI's scan summary and
guide whether a user commits the generator's output. Vague text undermines
the product.

- **explanation**: name the consequence, not just the pattern. "Returns
  exceptions to the model as opaque strings, so it can't recover" beats
  "raises exceptions".
- **fix**: prescribe the concrete change. "Pass `timeout=` (5–30s)" beats
  "add error handling".
- Match the tone of the surrounding rules — paragraph, no headers, no
  bullets.

## What this directory is NOT

- Not a place for ad-hoc YAML files. Every `.yaml` file here is loaded as
  a policy. Don't drop notes, examples, or work-in-progress files inline.
- Not a doc directory. The README.md and this CLAUDE.md are exceptions;
  they are filtered out by the loader's `.yaml`-suffix check, but the
  convention is "data only, two markdown docs only".
