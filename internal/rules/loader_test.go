package rules_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/rules"
)

// validYAML is a minimal valid policy file for loader tests.
const validYAML = `
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
    confidence: 0.8
    applies_to:
      - claude_sdk_tool
    match:
      has_docstring: true
    explanation: Some explanation.
    fix: Some fix.
`

func makeFS(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func TestLoader_ValidFile(t *testing.T) {
	fsys := makeFS(map[string]string{"test.yaml": validYAML})
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	if len(policies[0].Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(policies[0].Rules))
	}
	rule := policies[0].Rules[0]
	if rule.ID != "TEST-001" {
		t.Errorf("rule.ID = %q, want TEST-001", rule.ID)
	}
	if string(rule.Category) != "claude_sdk" {
		t.Errorf("rule.Category = %q, want claude_sdk", rule.Category)
	}
}

func TestLoader_MissingRequiredField(t *testing.T) {
	const noTitle = `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    severity: low
    confidence: 0.8
    applies_to:
      - claude_sdk_tool
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"bad.yaml": noTitle})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error should mention 'title', got: %v", err)
	}
}

func TestLoader_UnknownPredicateName(t *testing.T) {
	const badPredicate = `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    title: A rule
    severity: low
    confidence: 0.8
    applies_to:
      - claude_sdk_tool
    match:
      has_blah: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"bad.yaml": badPredicate})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for unknown predicate, got nil")
	}
}

func TestLoader_DuplicateRuleID(t *testing.T) {
	const dup1 = `
policy:
  id: p1
  name: P1
  category: claude_sdk
  description: x
rules:
  - id: DUP-001
    title: Rule A
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	const dup2 = `
policy:
  id: p2
  name: P2
  category: openshell
  description: x
rules:
  - id: DUP-001
    title: Rule B
    scope: tool
    severity: high
    confidence: 0.9
    applies_to: [shell_invocation]
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"p1.yaml": dup1, "p2.yaml": dup2})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for duplicate rule ID, got nil")
	}
	if !strings.Contains(err.Error(), "DUP-001") {
		t.Errorf("error should mention duplicate rule ID, got: %v", err)
	}
}

func TestLoad_RejectsMissingScope(t *testing.T) {
	fs := makeFS(map[string]string{
		"test/rule.yaml": `
policy:
  id: test
  name: Test
  category: claude_sdk
rules:
  - id: TEST-001
    title: Missing scope
    severity: low
    confidence: 0.5
    applies_to: [claude_sdk_tool]
    match: {has_docstring: false}
    explanation: x
    fix: x
`,
	})
	_, err := rules.Load(fs)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected scope-required error, got %v", err)
	}
}

func TestLoad_RejectsUnknownScope(t *testing.T) {
	fs := makeFS(map[string]string{
		"test/rule.yaml": `
policy:
  id: test
  name: Test
  category: claude_sdk
rules:
  - id: TEST-001
    title: Bad scope
    scope: tooooool
    severity: low
    confidence: 0.5
    applies_to: [claude_sdk_tool]
    match: {has_docstring: false}
    explanation: x
    fix: x
`,
	})
	_, err := rules.Load(fs)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected unknown-scope error, got %v", err)
	}
}

func TestLoad_SkipsManifestYAML(t *testing.T) {
	// A pack FS with a root manifest.yaml alongside a real policy file. Load
	// must ignore manifest.yaml, not try to decode it as a policy.
	fsys := fstest.MapFS{
		"manifest.yaml": &fstest.MapFile{Data: []byte("schema_version: 1\n")},
		"claude_sdk/x.yaml": &fstest.MapFile{Data: []byte(`policy:
  id: p
  name: P
  category: claude_sdk
  description: d
rules:
  - id: X-001
    title: t
    scope: tool
    severity: low
    confidence: 0.5
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: e
    fix: f
`)},
	}
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("len(policies) = %d, want 1 (manifest.yaml must be skipped)", len(policies))
	}
}
