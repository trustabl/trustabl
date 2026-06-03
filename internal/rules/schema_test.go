package rules_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/rules"
)

func TestSchemaUnmarshal_BasicRule(t *testing.T) {
	const src = `
policy:
  id: test_policy
  name: Test Policy
  category: claude_sdk
  description: A test policy.
rules:
  - id: TEST-001
    title: Test rule
    scope: tool
    severity: low
    confidence: 0.9
    applies_to:
      - claude_sdk_tool
    match:
      not:
        has_docstring: true
    explanation: Explanation text.
    fix: Fix text.
`
	var pf rules.PolicyFile
	if err := yaml.Unmarshal([]byte(src), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Policy.ID != "test_policy" {
		t.Errorf("policy.id = %q, want test_policy", pf.Policy.ID)
	}
	if len(pf.Rules) != 1 {
		t.Fatalf("len(rules) = %d, want 1", len(pf.Rules))
	}
	r := pf.Rules[0]
	if r.ID != "TEST-001" {
		t.Errorf("rule.id = %q, want TEST-001", r.ID)
	}
	if r.Match.Not == nil {
		t.Error("rule.match.not is nil")
	}
	if r.Match.Not.HasDocstring == nil || !*r.Match.Not.HasDocstring {
		t.Error("rule.match.not.has_docstring should be true")
	}
}
