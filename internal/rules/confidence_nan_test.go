package rules_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/rules"
)

// ruleWithConfidence builds a valid one-rule pack whose confidence value is
// interpolated raw, so a test can inject a YAML float literal ("0.8" or ".nan").
func ruleWithConfidence(confidence string) string {
	return `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: Test policy.
rules:
  - id: TEST-001
    title: A test rule
    scope: tool
    severity: low
    confidence: ` + confidence + `
    applies_to:
      - claude_sdk_tool
    match:
      has_docstring: true
    explanation: Some explanation.
    fix: Some fix.
`
}

// TestLoad_ConfidenceNaNRejected guards the validation fix: a YAML .nan
// confidence fails every ordered comparison, so a bare range check would let it
// through to poison scoring (NaN * weight = NaN). The positive control proves
// the pack is otherwise valid, isolating NaN as the cause of rejection.
func TestLoad_ConfidenceNaNRejected(t *testing.T) {
	okFS := fstest.MapFS{
		"test.yaml": {Data: []byte(ruleWithConfidence("0.8"))},
	}
	if _, err := rules.Load(okFS); err != nil {
		t.Fatalf("control pack with confidence 0.8 should load, got: %v", err)
	}

	nanFS := fstest.MapFS{
		"test.yaml": {Data: []byte(ruleWithConfidence(".nan"))},
	}
	_, err := rules.Load(nanFS)
	if err == nil {
		t.Fatal("Load accepted a NaN confidence; want a validation error")
	}
	if !strings.Contains(err.Error(), "confidence") {
		t.Errorf("error should mention confidence, got: %v", err)
	}
}
