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

// TestLoad_ScopeIndependentChecksRunOnInvalidScope proves the batch-all-errors
// intent (TR-154): a bad repo_has_sdk_in_code token is reported even when the
// rule's scope is invalid, because that check does not depend on the scope.
func TestLoad_ScopeIndependentChecksRunOnInvalidScope(t *testing.T) {
	const badScopeAndToken = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-001
    title: Invalid scope plus a category token where an SDK token is required
    scope: bogus
    severity: high
    confidence: 0.8
    applies_to: [openai_agents]
    match:
      repo_has_sdk_in_code: [claude_sdk]
    explanation: x
    fix: x
`
	_, err := rules.Load(makeFS(map[string]string{"bad.yaml": badScopeAndToken}))
	if err == nil {
		t.Fatal("expected errors, got nil")
	}
	if !strings.Contains(err.Error(), "unknown scope") {
		t.Errorf("expected the unknown-scope error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "repo_has_sdk_in_code") {
		t.Errorf("a bad SDK token must be reported even with an invalid scope, got: %v", err)
	}
}

// TestLoad_ScopeRequiredMessageListsSubagent guards the message fix (TR-154):
// the required-scope error must list all four scopes, including subagent.
func TestLoad_ScopeRequiredMessageListsSubagent(t *testing.T) {
	const noScope = `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    title: Missing scope
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	_, err := rules.Load(makeFS(map[string]string{"bad.yaml": noScope}))
	if err == nil {
		t.Fatal("expected error for missing scope, got nil")
	}
	if !strings.Contains(err.Error(), "subagent") {
		t.Errorf("required-scope message must list subagent, got: %v", err)
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

// TestLoad_RejectsEmptyCombinators rejects degenerate combinators: an empty
// any:/all: list and a not: wrapping an empty expression. `any: []` vacuously
// passes today (matches everything — the opposite of "at least one of these"),
// and `not: {}` is always false; both are authoring mistakes, not intent.
func TestLoad_RejectsEmptyCombinators(t *testing.T) {
	mk := func(matchBlock string) string {
		return `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    title: Degenerate combinator
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
` + matchBlock + `
    explanation: x
    fix: x
`
	}
	cases := map[string]string{
		"empty any":      "      any: []",
		"empty all":      "      all: []",
		"not over empty": `      not: {}`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := rules.Load(makeFS(map[string]string{"bad.yaml": mk(block)}))
			if err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}

// TestLoad_AcceptsEmptyTopLevelMatch confirms the guard does not break the
// legitimate singleton pattern: a repo rule with no match predicates (an empty
// top-level match) is gated purely by applies_to and must still load.
func TestLoad_AcceptsEmptyTopLevelMatch(t *testing.T) {
	const singleton = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-201
    title: Repo singleton, no predicate body
    scope: repo
    severity: medium
    confidence: 0.8
    applies_to: [openai_agents]
    match: {}
    explanation: x
    fix: x
`
	if _, err := rules.Load(makeFS(map[string]string{"ok.yaml": singleton})); err != nil {
		t.Fatalf("empty top-level match (singleton) rejected: %v", err)
	}
}

// TestLoad_RejectsEmptyMatchAtToolScope guards the footgun side of the empty
// match: at a per-instance scope an empty match fires on every tool/agent/
// subagent, which is almost always a forgotten predicate. The loader must reject
// it (only repo scope, the singleton pattern, may have an empty match).
func TestLoad_RejectsEmptyMatchAtToolScope(t *testing.T) {
	const emptyToolMatch = `
policy:
  id: test
  name: Test
  category: openai_sdk
  description: x
rules:
  - id: TEST-301
    title: Tool rule with no predicate body
    scope: tool
    severity: medium
    confidence: 0.8
    applies_to: [openai_tool]
    match: {}
    explanation: x
    fix: x
`
	_, err := rules.Load(makeFS(map[string]string{"bad.yaml": emptyToolMatch}))
	if err == nil || !strings.Contains(err.Error(), "empty match") {
		t.Fatalf("expected empty-match-at-tool-scope rejection, got %v", err)
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
