package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/rules"
)

// TestLoad_RejectsDeeplyNestedMatch guards the match-nesting bound: a rule whose
// match nests far past the limit (a hostile pack) must be rejected at load
// rather than blowing the stack in the loader's recursive validation walks or in
// the evaluator at scan time. 100 levels is comfortably past the engine's bound.
func TestLoad_RejectsDeeplyNestedMatch(t *testing.T) {
	const depth = 100
	indent := func(n int) string { return strings.Repeat("  ", n) }
	var body strings.Builder
	for i := 0; i < depth; i++ {
		body.WriteString(indent(i+3) + "not:\n")
	}
	body.WriteString(indent(depth+3) + "has_docstring: true\n")
	yaml := "policy:\n  id: deep\n  name: Deep\n  category: claude_sdk\n  description: d\n" +
		"rules:\n" +
		"  - id: DEEP-001\n    title: deep\n    scope: tool\n    severity: low\n" +
		"    confidence: 0.8\n    applies_to: [claude_sdk_tool]\n" +
		"    explanation: x\n    fix: y\n    match:\n" + body.String()
	if _, err := rules.Load(makeFS(map[string]string{"deep.yaml": yaml})); err == nil {
		t.Error("strict Load accepted a match nested past the depth bound; expected rejection")
	}
}
