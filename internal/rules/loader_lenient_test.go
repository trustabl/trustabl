package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/rules"
)

// twoRulePack: LEN-001 uses a known predicate; LEN-002 uses a predicate this
// build does not know (simulating a rule authored against a newer schema).
const twoRulePack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: Forward-compat test.
rules:
  - id: LEN-001
    title: Known predicate rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
  - id: LEN-002
    title: Future predicate rule
    scope: tool
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
    match:
      has_quantum_flux: true
    explanation: x
    fix: y
`

func ruleIDs(policies []rules.PolicyFile) map[string]bool {
	ids := map[string]bool{}
	for _, p := range policies {
		for _, r := range p.Rules {
			ids[r.ID] = true
		}
	}
	return ids
}

// TestLoadLenient_SkipsForwardIncompatibleRule is the core forward-compat
// contract: a rule referencing an unknown predicate is dropped whole (its ID
// returned in skipped) while its sibling loads, instead of failing the pack.
// The strict Load still rejects the same pack — typos are caught at authoring,
// only the runtime path degrades.
func TestLoadLenient_SkipsForwardIncompatibleRule(t *testing.T) {
	fsys := makeFS(map[string]string{"len.yaml": twoRulePack})

	policies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient unexpected error: %v", err)
	}
	ids := ruleIDs(policies)
	if !ids["LEN-001"] {
		t.Error("LEN-001 (known predicate) should have loaded")
	}
	if ids["LEN-002"] {
		t.Error("LEN-002 (unknown predicate) should have been skipped, not loaded")
	}
	if len(skipped) != 1 || skipped[0] != "LEN-002" {
		t.Errorf("skipped = %v, want [LEN-002]", skipped)
	}

	if _, err := rules.Load(fsys); err == nil {
		t.Error("strict Load must error on an unknown predicate key")
	} else if !strings.Contains(err.Error(), "has_quantum_flux") {
		t.Errorf("strict Load error should name the unknown field, got: %v", err)
	}
}

// TestLoadLenient_KnownOnlyPackMatchesStrict: a pack using only known
// predicates loads under lenient with zero skips.
func TestLoadLenient_KnownOnlyPackMatchesStrict(t *testing.T) {
	fsys := makeFS(map[string]string{"test.yaml": validYAML})
	policies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	if !ruleIDs(policies)["TEST-001"] {
		t.Error("TEST-001 should have loaded under lenient")
	}
}

// TestLoadLenient_UnknownInsideCombinator: an unknown predicate nested under an
// all/any/not combinator still drops the whole rule (the walk descends).
func TestLoadLenient_UnknownInsideCombinator(t *testing.T) {
	const pack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-010
    title: combinator
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      all:
        - has_docstring: true
        - has_quantum_flux: true
    explanation: x
    fix: y
`
	policies, skipped, err := rules.LoadLenient(makeFS(map[string]string{"c.yaml": pack}))
	if err != nil {
		t.Fatalf("LoadLenient: %v", err)
	}
	if ruleIDs(policies)["LEN-010"] {
		t.Error("LEN-010 should be skipped (unknown predicate inside all:)")
	}
	if len(skipped) != 1 || skipped[0] != "LEN-010" {
		t.Errorf("skipped = %v, want [LEN-010]", skipped)
	}
}

// TestLoadLenient_KnownNestedStructNotSkipped guards against over-skipping: a
// KNOWN predicate's nested struct keys (e.g. param_name_matches.exact) are NOT
// match-level keys, so the walk must not treat them as unknown.
func TestLoadLenient_KnownNestedStructNotSkipped(t *testing.T) {
	const pack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-020
    title: nested struct
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      param_name_matches:
        exact:
          - path
    explanation: x
    fix: y
`
	policies, skipped, err := rules.LoadLenient(makeFS(map[string]string{"n.yaml": pack}))
	if err != nil {
		t.Fatalf("LoadLenient: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none (param_name_matches is a known predicate)", skipped)
	}
	if !ruleIDs(policies)["LEN-020"] {
		t.Error("LEN-020 should load (nested struct keys are not unknown predicates)")
	}
}

// TestLoadLenient_MalformedYAMLStillErrors: leniency is about forward-compat,
// not corruption — malformed YAML is still a hard error.
func TestLoadLenient_MalformedYAMLStillErrors(t *testing.T) {
	fsys := makeFS(map[string]string{"bad.yaml": "policy: {id: x\nrules: [oops"})
	if _, _, err := rules.LoadLenient(fsys); err == nil {
		t.Error("LoadLenient must still error on malformed YAML")
	}
}
