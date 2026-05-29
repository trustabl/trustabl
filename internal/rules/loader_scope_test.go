package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/rules"
)

// TestLoad_RejectsOutOfScopePredicate guards against a rule whose match: sets a
// predicate that the rule's scope never evaluates. Such a clause is silently
// dropped at evaluation time (each Evaluate* method only dispatches its own
// scope's predicates), so the rule fires more broadly than authored — with no
// signal. The loader must reject it at load time.
func TestLoad_RejectsOutOfScopePredicate(t *testing.T) {
	// Agent-scoped rule that sets has_docstring (a TOOL predicate).
	const agentWithToolPred = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-001
    title: Agent rule with a tool predicate
    scope: agent
    severity: high
    confidence: 0.8
    applies_to: [openai_agent]
    match:
      agent_class: [Agent]
      has_docstring: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"bad.yaml": agentWithToolPred})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for out-of-scope predicate, got nil")
	}
	if !strings.Contains(err.Error(), "has_docstring") {
		t.Errorf("error should name the offending predicate has_docstring, got: %v", err)
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("error should name the scope, got: %v", err)
	}
}

// TestLoad_RejectsOutOfScopePredicateNested proves the check recurses into
// combinators — an out-of-scope predicate hidden under not:/all:/any: is still
// dead at evaluation and must be rejected.
func TestLoad_RejectsOutOfScopePredicateNested(t *testing.T) {
	const repoWithAgentPredNested = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-002
    title: Repo rule hiding an agent predicate under not
    scope: repo
    severity: medium
    confidence: 0.8
    applies_to: [openai_agents]
    match:
      all:
        - repo_has_sdk_in_code: [openai_agents]
        - not:
            agent_grants_builtin_tool: [Bash]
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"bad.yaml": repoWithAgentPredNested})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for nested out-of-scope predicate, got nil")
	}
	if !strings.Contains(err.Error(), "agent_grants_builtin_tool") {
		t.Errorf("error should name the offending nested predicate, got: %v", err)
	}
}

// TestLoad_AcceptsInScopePredicates is the companion: a rule using only its own
// scope's predicates (plus scope-agnostic combinators) loads cleanly.
func TestLoad_AcceptsInScopePredicates(t *testing.T) {
	const validAgent = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-003
    title: Well-scoped agent rule
    scope: agent
    severity: high
    confidence: 0.8
    applies_to: [openai_agent]
    match:
      all:
        - agent_class: [Agent]
        - not:
            agent_kwarg_present: [input_guardrails]
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"ok.yaml": validAgent})
	if _, err := rules.Load(fsys); err != nil {
		t.Fatalf("well-scoped agent rule rejected: %v", err)
	}
}
