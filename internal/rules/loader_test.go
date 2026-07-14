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

// TestLoader_MCPCategory verifies the mcp policy category loads. The trustabl-rules
// repo ships an mcp/ pack; the engine must accept category: mcp (regression for the
// drift where the rules repo shipped mcp rules no engine could load).
func TestLoader_MCPCategory(t *testing.T) {
	const mcpYAML = `
policy:
  id: mcp_test
  name: MCP Test
  category: mcp
  description: Test mcp policy.
rules:
  - id: MCP-999
    title: An mcp test rule
    scope: tool
    severity: low
    confidence: 0.8
    applies_to:
      - mcp_tool
    match:
      has_docstring: true
    explanation: Some explanation.
    fix: Some fix.
`
	fsys := makeFS(map[string]string{"mcp/test.yaml": mcpYAML})
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("loading an mcp-category policy should succeed, got: %v", err)
	}
	if len(policies) != 1 || len(policies[0].Rules) != 1 {
		t.Fatalf("expected 1 policy with 1 rule, got %+v", policies)
	}
	if string(policies[0].Rules[0].Category) != "mcp" {
		t.Errorf("rule.Category = %q, want mcp", policies[0].Rules[0].Category)
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

func TestLoader_RejectsConfidenceAboveOne(t *testing.T) {
	// confidence is a probability — schema.yaml documents the domain as (0, 1].
	// Scoring multiplies severity weight by confidence, so a value > 1 silently
	// inflates the risk score. The loader must reject it.
	const tooConfident = `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    title: A rule
    scope: tool
    severity: low
    confidence: 1.5
    applies_to:
      - claude_sdk_tool
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"bad.yaml": tooConfident})
	_, err := rules.Load(fsys)
	if err == nil {
		t.Fatal("expected error for confidence > 1, got nil")
	}
	if !strings.Contains(err.Error(), "confidence") {
		t.Errorf("error should mention 'confidence', got: %v", err)
	}
}

func TestLoader_AcceptsConfidenceOfExactlyOne(t *testing.T) {
	// 1.0 is the inclusive upper bound — a rule the engine is certain about
	// must still load.
	const certain = `
policy:
  id: test
  name: Test
  category: claude_sdk
  description: x
rules:
  - id: TEST-001
    title: A rule
    scope: tool
    severity: low
    confidence: 1.0
    applies_to:
      - claude_sdk_tool
    match:
      has_docstring: true
    explanation: x
    fix: x
`
	fsys := makeFS(map[string]string{"ok.yaml": certain})
	if _, err := rules.Load(fsys); err != nil {
		t.Fatalf("confidence of exactly 1.0 must load, got: %v", err)
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

func TestLoad_RejectsCategoryTokenInRepoHasSDKInCode(t *testing.T) {
	// repo_has_sdk_in_code matches SDK-enum tokens (claude_agent_sdk), NOT the
	// category token (claude_sdk) used by applies_to. A category token here
	// silently never matches; the loader must reject it.
	fs := makeFS(map[string]string{
		"test/rule.yaml": `
policy:
  id: test
  name: Test
  category: claude_sdk
rules:
  - id: TEST-201
    title: Repo rule with wrong SDK token
    scope: repo
    severity: low
    confidence: 0.5
    applies_to: [claude_sdk]
    match:
      repo_has_sdk_in_code: [claude_sdk]
    explanation: x
    fix: x
`,
	})
	_, err := rules.Load(fs)
	if err == nil || !strings.Contains(err.Error(), "repo_has_sdk_in_code") {
		t.Fatalf("expected repo_has_sdk_in_code token error, got %v", err)
	}
}

func TestLoad_AcceptsSDKEnumTokenInRepoHasSDKInCode(t *testing.T) {
	fs := makeFS(map[string]string{
		"test/rule.yaml": `
policy:
  id: test
  name: Test
  category: claude_sdk
rules:
  - id: TEST-201
    title: Repo rule with correct SDK token
    scope: repo
    severity: low
    confidence: 0.5
    applies_to: [claude_sdk]
    match:
      repo_has_sdk_in_code: [claude_agent_sdk]
    explanation: x
    fix: x
`,
	})
	if _, err := rules.Load(fs); err != nil {
		t.Fatalf("expected clean load for valid SDK-enum token, got %v", err)
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

func TestLoad_SkipsGitDir(t *testing.T) {
	// A cached pack is a git clone, so its root contains a .git/ tree. Any
	// *.yaml that lives under .git/ is VCS plumbing, not a policy, and must
	// never be decoded as a rule. The real policy alongside it still loads.
	fsys := fstest.MapFS{
		".git/config":               &fstest.MapFile{Data: []byte("[core]\n")},
		".git/refs/heads/main.yaml": &fstest.MapFile{Data: []byte("not: a policy\n")},
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
		t.Fatalf("len(policies) = %d, want 1 (.git/ subtree must be skipped)", len(policies))
	}
}

func TestLoad_SkipsMappingsDir(t *testing.T) {
	// mappings/ at the pack root holds compliance-framework mapping packs
	// (framework: + mappings: top-level keys), not detection policies. They do
	// not decode as PolicyFile, so if the walker descended them the load would
	// hard-fail on "policy.id is required" — in strict AND lenient modes
	// (required-field validation is deliberately strict in both). The subtree
	// must be skipped so a rules release that ships mapping packs cannot brick
	// a deployed engine.
	fsys := fstest.MapFS{
		"mappings/nist_800_53_r5.yaml": &fstest.MapFile{Data: []byte(`framework:
  id: nist_800_53_r5
  name: "NIST SP 800-53 Rev 5"
mappings:
  - rule_id: "CSDK-101"
    controls:
      - id: "AC-6"
`)},
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
		t.Fatalf("len(policies) = %d, want 1 (mappings/ subtree must be skipped)", len(policies))
	}
	// The lenient runtime path must skip it too — that is the path deployed
	// binaries take, and the one the mappings merge would otherwise break.
	lenPolicies, skipped, err := rules.LoadLenient(fsys)
	if err != nil {
		t.Fatalf("LoadLenient returned error: %v", err)
	}
	if len(lenPolicies) != 1 {
		t.Fatalf("LoadLenient len(policies) = %d, want 1", len(lenPolicies))
	}
	if len(skipped) != 0 {
		t.Fatalf("LoadLenient skipped = %v, want none (mappings files are not rules)", skipped)
	}
}
