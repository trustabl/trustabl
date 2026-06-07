package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/rules"
)

// unknownCategoryYAML is a structurally valid pack whose category this build
// does not recognize — i.e. a pack a newer rules release shipped for an SDK this
// binary predates. Its single rule uses a KNOWN predicate (has_docstring) so the
// lenient decoder does not drop it for an unknown predicate; the only reason it
// must be skipped is the category, which is what these tests exercise.
const unknownCategoryYAML = `policy:
  id: future
  name: Future SDK
  category: future_sdk
  description: a category this build does not know yet
rules:
  - id: FUT-001
    title: future rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    explanation: x
    fix: y
    match:
      has_docstring: true
`

// TestLoad_StrictRejectsUnknownCategory guards the authoring/CI contract: a
// pack with an unrecognized (or typo'd) category must still be a hard load
// error in strict mode, so a bad category is caught before it ships.
func TestLoad_StrictRejectsUnknownCategory(t *testing.T) {
	_, err := rules.Load(makeFS(map[string]string{"future.yaml": unknownCategoryYAML}))
	if err == nil {
		t.Fatal("strict Load accepted an unknown category; expected rejection")
	}
	if !strings.Contains(err.Error(), "unknown category") {
		t.Errorf("expected an unknown-category error, got: %v", err)
	}
}

// TestLoadLenient_SkipsUnknownCategory guards the forward-compat contract: the
// runtime loader must SKIP a pack whose category it doesn't know (recording its
// rule IDs as skipped) and still load the sibling pack it does know — so a newer
// rules release never blocks an older binary from scanning the SDKs it
// understands. Before this fix the unknown category hard-failed the whole load,
// taking every other SDK's rules down with it (the "unknown category" scan
// failure in the field).
func TestLoadLenient_SkipsUnknownCategory(t *testing.T) {
	policies, skipped, err := rules.LoadLenient(makeFS(map[string]string{
		"future.yaml": unknownCategoryYAML, // unrecognized category — must be skipped
		"known.yaml":  validYAML,           // recognized category (claude_sdk) — must load
	}))
	if err != nil {
		t.Fatalf("lenient Load errored on an unknown category (it must skip, not fail): %v", err)
	}

	loaded := map[string]bool{}
	for _, p := range policies {
		for _, r := range p.Rules {
			loaded[r.ID] = true
		}
	}
	if !loaded["TEST-001"] {
		t.Errorf("known-category rule TEST-001 was not loaded; got %v", loaded)
	}
	if loaded["FUT-001"] {
		t.Error("unknown-category rule FUT-001 was loaded; it must be skipped")
	}

	skippedSet := map[string]bool{}
	for _, id := range skipped {
		skippedSet[id] = true
	}
	if !skippedSet["FUT-001"] {
		t.Errorf("unknown-category rule FUT-001 was not recorded as skipped; got %v", skipped)
	}
}
