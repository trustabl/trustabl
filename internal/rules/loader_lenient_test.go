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

// TestLoadLenient_SkipsUnknownScope is the forward-compat contract for a NEW
// scope: a rule whose scope this build does not know — e.g. a future `skill`
// scope shipped by a newer rules release on a binary that predates skill support
// — is dropped whole (its ID returned in skipped) while its known-scope sibling
// loads. The unknown-scope rule deliberately uses a KNOWN predicate and a known
// applies_to value, so the ONLY reason it is skipped is the scope itself —
// isolating this from the predicate-drop path. Strict Load still rejects it, so
// the typo-vs-new ambiguity is resolved at authoring time, not at runtime.
func TestLoadLenient_SkipsUnknownScope(t *testing.T) {
	const pack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-100
    title: Known scope rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
  - id: LEN-101
    title: Future scope rule
    scope: skill
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`
	fsys := makeFS(map[string]string{"len.yaml": pack})
	policies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient unexpected error: %v", err)
	}
	ids := ruleIDs(policies)
	if !ids["LEN-100"] {
		t.Error("LEN-100 (known scope) should have loaded")
	}
	if ids["LEN-101"] {
		t.Error("LEN-101 (unknown scope `skill`) should have been skipped, not loaded")
	}
	if len(skipped) != 1 || skipped[0] != "LEN-101" {
		t.Errorf("skipped = %v, want [LEN-101]", skipped)
	}

	if _, err := rules.Load(fsys); err == nil {
		t.Error("strict Load must error on an unknown scope")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Errorf("strict Load error should name the scope problem, got: %v", err)
	}
}

// TestLoadLenient_SkipsUnknownAppliesTo: a rule with a KNOWN scope but an
// applies_to value this build does not recognize (a tool/agent kind a newer
// engine added) is skipped at runtime, not hard-failed, while a sibling loads.
// As with predicates and scope, strict Load rejects it so an authoring typo is
// caught in CI rather than silently degrading every deployed scan.
func TestLoadLenient_SkipsUnknownAppliesTo(t *testing.T) {
	const pack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-110
    title: Known applies_to rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
  - id: LEN-111
    title: Future applies_to rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [future_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`
	fsys := makeFS(map[string]string{"len.yaml": pack})
	policies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient unexpected error: %v", err)
	}
	ids := ruleIDs(policies)
	if !ids["LEN-110"] {
		t.Error("LEN-110 (known applies_to) should have loaded")
	}
	if ids["LEN-111"] {
		t.Error("LEN-111 (unknown applies_to `future_sdk_tool`) should have been skipped")
	}
	if len(skipped) != 1 || skipped[0] != "LEN-111" {
		t.Errorf("skipped = %v, want [LEN-111]", skipped)
	}

	if _, err := rules.Load(fsys); err == nil {
		t.Error("strict Load must error on an unknown applies_to value")
	}
}

// TestLoadLenient_MalformedKnownRuleStillErrors is the AC#3 regression: lenient
// loading is forward-compat ONLY — it must NOT swallow a real authoring error in
// a rule this build fully understands. A rule with a known scope, known
// applies_to, and a known predicate but a genuine defect still hard-fails
// LoadLenient, exactly as strict Load does. This is the "not a license to
// silently drop real authoring errors" line from the ticket: only an UNKNOWN
// scope/applies_to/predicate is forward-incompatible; a malformed KNOWN rule is
// a defect, not a newer-engine signal.
func TestLoadLenient_MalformedKnownRuleStillErrors(t *testing.T) {
	cases := map[string]string{
		// Missing the required `explanation` field.
		"missing required field": `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-120
    title: Known but missing explanation
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    fix: y
`,
		// Confidence outside the (0,1] probability range.
		"out-of-range confidence": `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-121
    title: Known but bad confidence
    scope: tool
    severity: low
    confidence: 1.7
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`,
	}
	for name, pack := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := makeFS(map[string]string{"m.yaml": pack})
			if _, _, err := rules.LoadLenient(fsys); err == nil {
				t.Errorf("LoadLenient must still hard-fail a malformed KNOWN rule (%s)", name)
			}
			if _, err := rules.Load(fsys); err == nil {
				t.Errorf("strict Load must also hard-fail a malformed KNOWN rule (%s)", name)
			}
		})
	}
}

// TestLoadLenient_SkipsUnknownLanguage is the forward-compat contract for a NEW
// language, and the regression guard for the v0.1.3 break: when csharp/php/rust
// rules were merged, an older binary whose loader did not know those languages
// hard-failed the ENTIRE rule load right after inventory — because the language
// check rejected unknown values on the lenient runtime path too, unlike scope /
// applies_to / predicates. Now a rule whose `language:` this build cannot produce
// is dropped whole (its ID returned in skipped) while its known-language sibling
// loads. Strict Load still rejects it so an authoring typo is caught in CI.
func TestLoadLenient_SkipsUnknownLanguage(t *testing.T) {
	const pack = `
policy:
  id: len
  name: Lenient
  category: claude_sdk
  description: t
rules:
  - id: LEN-200
    title: Known language rule
    scope: tool
    severity: low
    confidence: 0.8
    language: go
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
  - id: LEN-201
    title: Future language rule
    scope: tool
    severity: high
    confidence: 0.9
    language: ruby
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`
	fsys := makeFS(map[string]string{"len.yaml": pack})
	policies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient unexpected error: %v", err)
	}
	ids := ruleIDs(policies)
	if !ids["LEN-200"] {
		t.Error("LEN-200 (known language go) should have loaded")
	}
	if ids["LEN-201"] {
		t.Error("LEN-201 (unknown language `ruby`) should have been skipped, not loaded")
	}
	if len(skipped) != 1 || skipped[0] != "LEN-201" {
		t.Errorf("skipped = %v, want [LEN-201]", skipped)
	}

	if _, err := rules.Load(fsys); err == nil {
		t.Error("strict Load must error on an unknown language")
	} else if !strings.Contains(err.Error(), "language") {
		t.Errorf("strict Load error should name the language problem, got: %v", err)
	}
}
