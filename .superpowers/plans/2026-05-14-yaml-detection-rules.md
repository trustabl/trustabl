# YAML Detection Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all hardcoded Go detector structs with declarative YAML policy files and a parallel execution engine, so contributors add rules by writing YAML rather than Go.

**Architecture:** A new `internal/rules/` package provides schema types, a loader (validated YAML → `[]PolicyFile`), predicates (one function per detection primitive), and an evaluator (recursive combinator walker). A single `RuleDetector` wraps a `RuleDef` and implements the existing `Detector` interface. `Registry.Run` gains a bounded goroutine worker pool. The scanner switches from `NewRegistry` to `LoadRegistry(fs.FS)`.

**Tech Stack:** `gopkg.in/yaml.v3` (already in go.mod), `io/fs` + `embed`, `sync` + `runtime` for parallelism, `github.com/smacker/go-tree-sitter` for AST predicates.

---

## File Layout

```
internal/rules/
  schema.go          — Go structs mirroring the YAML
  loader.go          — fs.FS → []PolicyFile with full validation
  predicates.go      — one function per detection primitive
  evaluator.go       — Evaluate(MatchExpr, ToolDef, ParsedFile) bool
  embed.go           — //go:embed + DefaultFS
  policies/
    claude_sdk.yaml  — CSDK-001 through CSDK-007
    openshell.yaml   — OSH-001 through OSH-005
  schema_test.go
  loader_test.go
  predicates_test.go
  evaluator_test.go
  policies_test.go

internal/analysis/detectors/detector.go  — add RuleDetector, LoadRegistry, parallel Run
internal/scanner/scanner.go              — add RulesDir to Config, switch to LoadRegistry
cmd/karenctl/main.go                     — add --rules-dir flag

(deleted at end)
internal/analysis/detectors/claude_sdk.go
internal/analysis/detectors/openshell.go
internal/analysis/detectors/detectors_test.go
```

---

## Task 1: Schema types

**Files:**
- Create: `internal/rules/schema.go`
- Create: `internal/rules/schema_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/rules/schema_test.go`:

```go
package rules_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/karenctl/internal/rules"
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
    severity: low
    confidence: 0.9
    applies_to:
      - claude_sdk_tool
    singleton: false
    match:
      not:
        has_docstring: true
    explanation: Explanation text.
    fix: Fix text.
    fix_hints:
      add_docstring: true
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
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/rules/... -run TestSchemaUnmarshal_BasicRule
```

Expected: compile error — package `rules` does not exist.

- [ ] **Step 3: Create `internal/rules/schema.go`**

```go
package rules

import "github.com/trustabl/karenctl/internal/models"

// PolicyFile is the top-level structure of a .yaml policy file.
type PolicyFile struct {
	Policy PolicyMeta `yaml:"policy"`
	Rules  []RuleDef  `yaml:"rules"`
}

// PolicyMeta holds the policy-level metadata.
type PolicyMeta struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Category    string `yaml:"category"` // maps to models.DetectorCategory
	Description string `yaml:"description"`
}

// RuleDef is one rule entry inside a policy file.
// Category is not in YAML — the loader copies it from PolicyMeta.Category.
type RuleDef struct {
	ID          string                  `yaml:"id"`
	Title       string                  `yaml:"title"`
	Severity    models.Severity         `yaml:"severity"`
	Confidence  float64                 `yaml:"confidence"`
	AppliesTo   []string                `yaml:"applies_to"`
	Singleton   bool                    `yaml:"singleton"`
	Match       MatchExpr               `yaml:"match"`
	Explanation string                  `yaml:"explanation"`
	Fix         string                  `yaml:"fix"`
	FixHints    map[string]any          `yaml:"fix_hints,omitempty"`
	Category    models.DetectorCategory `yaml:"-"` // populated by loader
}

// MatchExpr is a recursive predicate or combinator. All set fields are ANDed.
type MatchExpr struct {
	// Combinators
	All []MatchExpr `yaml:"all,omitempty"`
	Any []MatchExpr `yaml:"any,omitempty"`
	Not *MatchExpr  `yaml:"not,omitempty"`

	// Bool predicates — pointer distinguishes "set to false" from "absent"
	HasDocstring      *bool `yaml:"has_docstring,omitempty"`
	HasParams         *bool `yaml:"has_params,omitempty"`
	HasTypedParams    *bool `yaml:"has_typed_params,omitempty"`
	HasRaise          *bool `yaml:"has_raise,omitempty"`
	HasTryExcept      *bool `yaml:"has_try_except,omitempty"`
	HasShellCall      *bool `yaml:"has_shell_call,omitempty"`
	HasWriteCall      *bool `yaml:"has_write_call,omitempty"`
	HasDynamicURLCall *bool `yaml:"has_dynamic_url_call,omitempty"`
	Always            *bool `yaml:"always,omitempty"`

	// String-list predicates
	NameIn        []string `yaml:"name_in,omitempty"`
	NameHasPrefix []string `yaml:"name_has_prefix,omitempty"`
	HasBodyText   []string `yaml:"has_body_text,omitempty"`

	// Nested struct predicates
	ParamNameMatches   *ParamNameMatchExpr    `yaml:"param_name_matches,omitempty"`
	CallWithoutKwarg   *CallWithoutKwargExpr  `yaml:"call_without_kwarg,omitempty"`
	CallWithKwargValue *CallWithKwargValueExpr `yaml:"call_with_kwarg_value,omitempty"`
	CallUsesParam      *CallUsesParamExpr      `yaml:"call_uses_param,omitempty"`
}

// ParamNameMatchExpr matches parameter names against exact/contains/suffix/prefix patterns.
type ParamNameMatchExpr struct {
	Exact    []string `yaml:"exact,omitempty"`
	Contains []string `yaml:"contains,omitempty"`
	Suffixes []string `yaml:"suffixes,omitempty"`
	Prefixes []string `yaml:"prefixes,omitempty"`
}

// CallWithoutKwargExpr fires when a matching call is missing the named keyword argument.
type CallWithoutKwargExpr struct {
	Callees []string `yaml:"callees"`
	Missing string   `yaml:"missing"`
}

// CallWithKwargValueExpr fires when a matching call has kwarg == value.
type CallWithKwargValueExpr struct {
	CalleePrefix string   `yaml:"callee_prefix,omitempty"`
	Callees      []string `yaml:"callees,omitempty"`
	Kwarg        string   `yaml:"kwarg"`
	Value        string   `yaml:"value"`
}

// CallUsesParamExpr fires when a matching call receives a path-like param as an arg.
type CallUsesParamExpr struct {
	Callees        []string `yaml:"callees,omitempty"`
	CalleePrefix   string   `yaml:"callee_prefix,omitempty"`
	CalleePrefixes []string `yaml:"callee_prefixes,omitempty"`
}
```

- [ ] **Step 4: Run test to confirm it passes**

```
go test ./internal/rules/... -run TestSchemaUnmarshal_BasicRule -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/rules/schema.go internal/rules/schema_test.go
git commit -m "feat(rules): add YAML schema types for policies and rules"
```

---

## Task 2: Loader

**Files:**
- Create: `internal/rules/loader.go`
- Create: `internal/rules/loader_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/loader_test.go`:

```go
package rules_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trustabl/karenctl/internal/rules"
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
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/rules/... -run "TestLoader"
```

Expected: compile error — `rules.Load` undefined.

- [ ] **Step 3: Create `internal/rules/loader.go`**

```go
package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/karenctl/internal/models"
)

// Load reads all .yaml files from fsys, unmarshals and validates each, and
// returns all policy files. All errors are collected — not fail-fast — so a
// contributor sees every problem in one run.
func Load(fsys fs.FS) ([]PolicyFile, error) {
	entries, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}

	var (
		policies []PolicyFile
		errs     []error
		seenIDs  = map[string]string{} // rule ID → file that defined it
	)

	for _, name := range entries {
		f, err := fsys.Open(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: open: %w", name, err))
			continue
		}

		var pf PolicyFile
		dec := yaml.NewDecoder(f)
		dec.KnownFields(true)
		decErr := dec.Decode(&pf)
		f.Close()

		if decErr != nil {
			errs = append(errs, fmt.Errorf("%s: decode: %w", name, decErr))
			continue
		}

		// Validate policy-level required fields.
		if pf.Policy.ID == "" {
			errs = append(errs, fmt.Errorf("%s: policy.id is required", name))
		}
		if pf.Policy.Category == "" {
			errs = append(errs, fmt.Errorf("%s: policy.category is required", name))
		}

		for i, rule := range pf.Rules {
			tag := fmt.Sprintf("%s rule[%d]", name, i)
			if rule.ID != "" {
				tag = fmt.Sprintf("%s rule %s", name, rule.ID)
			}
			if rule.ID == "" {
				errs = append(errs, fmt.Errorf("%s: id is required", tag))
			}
			if rule.Title == "" {
				errs = append(errs, fmt.Errorf("%s: title is required", tag))
			}
			if rule.Severity == "" {
				errs = append(errs, fmt.Errorf("%s: severity is required", tag))
			}
			if rule.Confidence <= 0 {
				errs = append(errs, fmt.Errorf("%s: confidence is required (must be > 0)", tag))
			}
			if len(rule.AppliesTo) == 0 {
				errs = append(errs, fmt.Errorf("%s: applies_to is required", tag))
			}
			if rule.Explanation == "" {
				errs = append(errs, fmt.Errorf("%s: explanation is required", tag))
			}
			if rule.Fix == "" {
				errs = append(errs, fmt.Errorf("%s: fix is required", tag))
			}
			if rule.ID != "" {
				if prev, seen := seenIDs[rule.ID]; seen {
					errs = append(errs, fmt.Errorf("duplicate rule ID %q in %s (previously defined in %s)", rule.ID, name, prev))
				} else {
					seenIDs[rule.ID] = name
				}
			}
			// Populate category from policy metadata — not in YAML.
			pf.Rules[i].Category = models.DetectorCategory(pf.Policy.Category)
		}
		policies = append(policies, pf)
	}

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, errors.New(strings.Join(msgs, "\n"))
	}
	return policies, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/rules/... -run "TestLoader" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```
git add internal/rules/loader.go internal/rules/loader_test.go
git commit -m "feat(rules): add YAML policy loader with field and duplicate-ID validation"
```

---

## Task 3: Metadata and string-list predicates

**Files:**
- Create: `internal/rules/predicates.go`
- Create: `internal/rules/predicates_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/predicates_test.go`:

```go
package rules_test

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/analysis/astutil"
	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/rules"
)

// parsePy parses a Python snippet and returns ParsedFile + ToolDef for the first
// function. kind defaults to claude_sdk_tool.
func parsePy(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	b := []byte(src)
	tree, err := astutil.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "test.py", Source: b, Tree: tree}
	fns := astutil.FindAll(tree.RootNode(), "function_definition", "decorated_definition")
	if len(fns) == 0 {
		t.Fatal("no function found")
	}
	fn := astutil.FunctionDef(fns[0])
	if fn == nil {
		fn = fns[0]
	}
	name := astutil.FunctionName(fn, b)
	doc := astutil.FunctionDocstring(fn, b)
	params := astutil.FunctionParams(fn, b)
	filtered := params[:0]
	for _, p := range params {
		if p != "self" && p != "cls" {
			filtered = append(filtered, p)
		}
	}
	tool := models.ToolDef{
		Name:           name,
		Kind:           kind,
		FilePath:       pf.RelPath,
		Line:           astutil.NodeLine(fn),
		EndLine:        astutil.NodeEndLine(fn),
		Description:    doc,
		HasInputSchema: astutil.FunctionHasTypedParams(fn),
		ParamNames:     filtered,
		RawSource:      astutil.NodeText(fn, b),
		Facts:          map[string]string{},
	}
	return tool, pf
}

// ─── has_docstring ────────────────────────────────────────────────────────────

func TestPred_HasDocstring_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    """Does stuff."""
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasDocstring(tool) {
		t.Error("expected HasDocstring true")
	}
}

func TestPred_HasDocstring_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasDocstring(tool) {
		t.Error("expected HasDocstring false")
	}
}

// ─── has_params ───────────────────────────────────────────────────────────────

func TestPred_HasParams_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasParams(tool) {
		t.Error("expected HasParams true")
	}
}

func TestPred_HasParams_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo() -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasParams(tool) {
		t.Error("expected HasParams false for no-param function")
	}
}

// ─── has_typed_params ─────────────────────────────────────────────────────────

func TestPred_HasTypedParams_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasTypedParams(tool) {
		t.Error("expected HasTypedParams true")
	}
}

func TestPred_HasTypedParams_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x, y):
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasTypedParams(tool) {
		t.Error("expected HasTypedParams false")
	}
}

// ─── has_raise ────────────────────────────────────────────────────────────────

func TestPred_HasRaise_True(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    if not x:
        raise ValueError("empty")
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasRaise(tool, pf) {
		t.Error("expected HasRaise true")
	}
}

func TestPred_HasRaise_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasRaise(tool, pf) {
		t.Error("expected HasRaise false")
	}
}

// ─── has_try_except ───────────────────────────────────────────────────────────

func TestPred_HasTryExcept_True(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    try:
        return {"x": x}
    except Exception as e:
        return {"error": str(e)}
`, models.KindClaudeSDKTool)
	if !rules.PredHasTryExcept(tool, pf) {
		t.Error("expected HasTryExcept true")
	}
}

func TestPred_HasTryExcept_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasTryExcept(tool, pf) {
		t.Error("expected HasTryExcept false")
	}
}

// ─── name_in ──────────────────────────────────────────────────────────────────

func TestPred_NameIn_Hit(t *testing.T) {
	tool := models.ToolDef{Name: "process"}
	if !rules.PredNameIn([]string{"process", "handle"}, tool) {
		t.Error("expected NameIn hit for 'process'")
	}
}

func TestPred_NameIn_Miss(t *testing.T) {
	tool := models.ToolDef{Name: "summarize_invoice"}
	if rules.PredNameIn([]string{"process", "handle"}, tool) {
		t.Error("expected NameIn miss")
	}
}

// ─── name_has_prefix ──────────────────────────────────────────────────────────

func TestPred_NameHasPrefix_Hit(t *testing.T) {
	tool := models.ToolDef{Name: "create_order"}
	if !rules.PredNameHasPrefix([]string{"create_", "send_"}, tool) {
		t.Error("expected NameHasPrefix hit")
	}
}

func TestPred_NameHasPrefix_Miss(t *testing.T) {
	tool := models.ToolDef{Name: "get_order"}
	if rules.PredNameHasPrefix([]string{"create_", "send_"}, tool) {
		t.Error("expected NameHasPrefix miss")
	}
}

// ─── param_name_matches ───────────────────────────────────────────────────────

func TestPred_ParamNameMatches_ExactHit(t *testing.T) {
	tool := models.ToolDef{ParamNames: []string{"path"}}
	expr := rules.ParamNameMatchExpr{Exact: []string{"path", "file"}}
	if !rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches hit on exact 'path'")
	}
}

func TestPred_ParamNameMatches_SuffixHit(t *testing.T) {
	tool := models.ToolDef{ParamNames: []string{"output_path"}}
	expr := rules.ParamNameMatchExpr{Suffixes: []string{"_path", "_file"}}
	if !rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches hit on suffix '_path'")
	}
}

func TestPred_ParamNameMatches_Miss_SubstringOnly(t *testing.T) {
	// "editor_id" contains "dir" but should NOT match suffix "_dir"
	tool := models.ToolDef{ParamNames: []string{"editor_id"}}
	expr := rules.ParamNameMatchExpr{Suffixes: []string{"_dir"}}
	if rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches miss for 'editor_id' vs '_dir' suffix")
	}
}

// ─── has_body_text ────────────────────────────────────────────────────────────

func TestPred_HasBodyText_Hit(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(p: str) -> str:
    """Foo."""
    return Path(p).resolve()
`, models.KindClaudeSDKTool)
	if !rules.PredHasBodyText([]string{".resolve(", "realpath("}, tool, pf) {
		t.Error("expected HasBodyText hit for '.resolve('")
	}
}

func TestPred_HasBodyText_Miss(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(p: str) -> str:
    """Foo."""
    return open(p).read()
`, models.KindClaudeSDKTool)
	if rules.PredHasBodyText([]string{".resolve(", "realpath("}, tool, pf) {
		t.Error("expected HasBodyText miss")
	}
}

// findFunctionNode is a local helper for predicates that need a sitter.Node.
// It mirrors the helper in predicates.go.
var _ = (*sitter.Node)(nil) // ensure sitter is importable
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/rules/... -run "TestPred_"
```

Expected: compile error — `rules.PredHasDocstring` etc. undefined.

- [ ] **Step 3: Create `internal/rules/predicates.go`**

```go
package rules

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/analysis/astutil"
	"github.com/trustabl/karenctl/internal/models"
)

// ─── bool predicates ─────────────────────────────────────────────────────────

func PredHasDocstring(t models.ToolDef) bool {
	return strings.TrimSpace(t.Description) != ""
}

func PredHasParams(t models.ToolDef) bool {
	return len(t.ParamNames) > 0
}

func PredHasTypedParams(t models.ToolDef) bool {
	return t.HasInputSchema
}

func PredHasRaise(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	return len(astutil.FindAll(root, "raise_statement")) > 0
}

func PredHasTryExcept(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	return len(astutil.FindAll(root, "try_statement")) > 0
}

func PredHasShellCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		c := astutil.NodeText(fn, pf.Source)
		if strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" {
			found = true
			return false
		}
		return true
	})
	return found
}

func PredHasWriteCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		if callee == "open" {
			args := n.ChildByFieldName("arguments")
			if args != nil {
				text := astutil.NodeText(args, pf.Source)
				if strings.Contains(text, `"w"`) || strings.Contains(text, `'w'`) ||
					strings.Contains(text, `"a"`) || strings.Contains(text, `'a'`) ||
					strings.Contains(text, `"x"`) || strings.Contains(text, `'x'`) {
					found = true
					return false
				}
			}
			return true
		}
		if callee == "shutil.copy" || callee == "shutil.copy2" ||
			callee == "shutil.move" || callee == "shutil.rmtree" {
			found = true
			return false
		}
		return true
	})
	return found
}

func PredHasDynamicURLCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		if !isHTTPCall(astutil.NodeText(fn, pf.Source)) {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		if int(args.NamedChildCount()) > 0 {
			first := args.NamedChild(0)
			if first.Type() != "string" {
				found = true
			} else {
				for i := 0; i < int(first.NamedChildCount()); i++ {
					if first.NamedChild(i).Type() == "interpolation" {
						found = true
						break
					}
				}
			}
		}
		return !found
	})
	return found
}

// ─── string-list predicates ───────────────────────────────────────────────────

func PredNameIn(names []string, t models.ToolDef) bool {
	lower := strings.ToLower(t.Name)
	for _, n := range names {
		if lower == strings.ToLower(n) {
			return true
		}
	}
	return false
}

func PredNameHasPrefix(prefixes []string, t models.ToolDef) bool {
	lower := strings.ToLower(t.Name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func PredHasBodyText(needles []string, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	body := astutil.NodeText(root, pf.Source)
	for _, needle := range needles {
		if strings.Contains(body, needle) {
			return true
		}
	}
	return false
}

func PredParamNameMatches(expr ParamNameMatchExpr, t models.ToolDef) bool {
	for _, p := range t.ParamNames {
		lower := strings.ToLower(p)
		for _, e := range expr.Exact {
			if lower == strings.ToLower(e) {
				return true
			}
		}
		for _, c := range expr.Contains {
			if strings.Contains(lower, strings.ToLower(c)) {
				return true
			}
		}
		for _, s := range expr.Suffixes {
			if strings.HasSuffix(lower, strings.ToLower(s)) {
				return true
			}
		}
		for _, pr := range expr.Prefixes {
			if strings.HasPrefix(lower, strings.ToLower(pr)) {
				return true
			}
		}
	}
	return false
}

// ─── call-site predicates ─────────────────────────────────────────────────────

func PredCallWithoutKwarg(expr CallWithoutKwargExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		if _, ok := calleeSet[astutil.NodeText(fn, pf.Source)]; !ok {
			return true
		}
		if !hasKwarg(n, pf.Source, expr.Missing) {
			found = true
		}
		return !found
	})
	return found
}

func PredCallWithKwargValue(expr CallWithKwargValueExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		matches := false
		if _, ok := calleeSet[callee]; ok {
			matches = true
		}
		if !matches && expr.CalleePrefix != "" && strings.HasPrefix(callee, expr.CalleePrefix) {
			matches = true
		}
		if !matches {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		astutil.Walk(args, func(kn *sitter.Node) bool {
			if kn.Type() != "keyword_argument" {
				return true
			}
			kname := kn.ChildByFieldName("name")
			kval := kn.ChildByFieldName("value")
			if kname == nil || kval == nil {
				return true
			}
			if astutil.NodeText(kname, pf.Source) == expr.Kwarg &&
				astutil.NodeText(kval, pf.Source) == expr.Value {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}

func PredCallUsesParam(expr CallUsesParamExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := findFunctionNode(t, pf)
	if root == nil {
		return false
	}
	pathish := make(map[string]struct{})
	for _, p := range t.ParamNames {
		if isPathishParam(p) {
			pathish[p] = struct{}{}
		}
	}
	if len(pathish) == 0 {
		return false
	}
	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		matches := false
		if _, ok := calleeSet[callee]; ok {
			matches = true
		}
		if !matches && expr.CalleePrefix != "" && strings.HasPrefix(callee, expr.CalleePrefix) {
			matches = true
		}
		if !matches {
			for _, pref := range expr.CalleePrefixes {
				if strings.HasPrefix(callee, pref) {
					matches = true
					break
				}
			}
		}
		if !matches {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		astutil.Walk(args, func(arg *sitter.Node) bool {
			if arg.Type() == "identifier" {
				if _, ok := pathish[astutil.NodeText(arg, pf.Source)]; ok {
					found = true
					return false
				}
			}
			return true
		})
		return !found
	})
	return found
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func findFunctionNode(t models.ToolDef, pf analysis.ParsedFile) *sitter.Node {
	var match *sitter.Node
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if match != nil {
			return false
		}
		if n.Type() != "function_definition" {
			return true
		}
		if astutil.NodeLine(n) == t.Line && astutil.FunctionName(n, pf.Source) == t.Name {
			match = n
			return false
		}
		return true
	})
	return match
}

func hasKwarg(call *sitter.Node, src []byte, name string) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	found := false
	astutil.Walk(args, func(n *sitter.Node) bool {
		if n.Type() != "keyword_argument" {
			return true
		}
		k := n.ChildByFieldName("name")
		if k != nil && astutil.NodeText(k, src) == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func isHTTPCall(callee string) bool {
	switch callee {
	case "requests.get", "requests.post", "requests.put", "requests.delete",
		"requests.patch", "requests.head", "requests.request",
		"requests.Session.get", "requests.Session.post",
		"httpx.get", "httpx.post", "httpx.put", "httpx.delete",
		"httpx.patch", "httpx.head", "httpx.request",
		"httpx.AsyncClient", "httpx.Client",
		"urllib.request.urlopen", "aiohttp.ClientSession.get",
		"aiohttp.ClientSession.post":
		return true
	}
	return false
}

func isPathishParam(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "path", "file", "filename", "filepath", "dir", "directory":
		return true
	}
	return strings.HasSuffix(lower, "_path") ||
		strings.HasSuffix(lower, "_file") ||
		strings.HasSuffix(lower, "_dir") ||
		strings.HasSuffix(lower, "_directory") ||
		strings.HasPrefix(lower, "file_") ||
		strings.HasPrefix(lower, "path_")
}
```

- [ ] **Step 4: Add call-site predicate tests to `predicates_test.go`**

Append these tests to `predicates_test.go`:

```go
// ─── has_shell_call ───────────────────────────────────────────────────────────

func TestPred_HasShellCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    subprocess.run([cmd])
    return "done"
`, models.KindShellInvocation)
	if !rules.PredHasShellCall(tool, pf) {
		t.Error("expected HasShellCall true")
	}
}

func TestPred_HasShellCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> str:
    """Foo."""
    return x
`, models.KindClaudeSDKTool)
	if rules.PredHasShellCall(tool, pf) {
		t.Error("expected HasShellCall false")
	}
}

// ─── has_write_call ───────────────────────────────────────────────────────────

func TestPred_HasWriteCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
def write(name: str) -> str:
    """Write."""
    with open(f"/tmp/{name}", "w") as f:
        f.write("data")
    return "ok"
`, models.KindShellInvocation)
	if !rules.PredHasWriteCall(tool, pf) {
		t.Error("expected HasWriteCall true")
	}
}

func TestPred_HasWriteCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
def read(name: str) -> str:
    """Read."""
    with open(f"/tmp/{name}", "r") as f:
        return f.read()
`, models.KindShellInvocation)
	if rules.PredHasWriteCall(tool, pf) {
		t.Error("expected HasWriteCall false for read-only open")
	}
}

// ─── has_dynamic_url_call ─────────────────────────────────────────────────────

func TestPred_HasDynamicURLCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def fetch(url: str) -> dict:
    """Fetch."""
    return requests.get(url).json()
`, models.KindClaudeSDKTool)
	if !rules.PredHasDynamicURLCall(tool, pf) {
		t.Error("expected HasDynamicURLCall true")
	}
}

func TestPred_HasDynamicURLCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def fetch() -> dict:
    """Fetch."""
    return requests.get("https://api.example.com/data").json()
`, models.KindClaudeSDKTool)
	if rules.PredHasDynamicURLCall(tool, pf) {
		t.Error("expected HasDynamicURLCall false for literal URL")
	}
}

// ─── call_without_kwarg ───────────────────────────────────────────────────────

func TestPred_CallWithoutKwarg_True(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id).json()
`, models.KindClaudeSDKTool)
	expr := rules.CallWithoutKwargExpr{
		Callees: []string{"requests.get", "requests.post"},
		Missing: "timeout",
	}
	if !rules.PredCallWithoutKwarg(expr, tool, pf) {
		t.Error("expected CallWithoutKwarg true")
	}
}

func TestPred_CallWithoutKwarg_False(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id, timeout=10).json()
`, models.KindClaudeSDKTool)
	expr := rules.CallWithoutKwargExpr{
		Callees: []string{"requests.get"},
		Missing: "timeout",
	}
	if rules.PredCallWithoutKwarg(expr, tool, pf) {
		t.Error("expected CallWithoutKwarg false when timeout is present")
	}
}

// ─── call_with_kwarg_value ────────────────────────────────────────────────────

func TestPred_CallWithKwargValue_True(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(name: str) -> str:
    """Run."""
    subprocess.run(f"cmd {name}", shell=True)
    return "done"
`, models.KindShellInvocation)
	expr := rules.CallWithKwargValueExpr{
		CalleePrefix: "subprocess.",
		Kwarg:        "shell",
		Value:        "True",
	}
	if !rules.PredCallWithKwargValue(expr, tool, pf) {
		t.Error("expected CallWithKwargValue true for shell=True")
	}
}

func TestPred_CallWithKwargValue_False(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(name: str) -> str:
    """Run."""
    subprocess.run(["cmd", name])
    return "done"
`, models.KindShellInvocation)
	expr := rules.CallWithKwargValueExpr{
		CalleePrefix: "subprocess.",
		Kwarg:        "shell",
		Value:        "True",
	}
	if rules.PredCallWithKwargValue(expr, tool, pf) {
		t.Error("expected CallWithKwargValue false for list-form call")
	}
}

// ─── call_uses_param ──────────────────────────────────────────────────────────

func TestPred_CallUsesParam_True(t *testing.T) {
	tool, pf := parsePy(t, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, models.KindClaudeSDKTool)
	expr := rules.CallUsesParamExpr{
		Callees:        []string{"open", "Path"},
		CalleePrefixes: []string{"os.", "shutil."},
	}
	if !rules.PredCallUsesParam(expr, tool, pf) {
		t.Error("expected CallUsesParam true")
	}
}

func TestPred_CallUsesParam_False_NoPathishParam(t *testing.T) {
	tool, pf := parsePy(t, `
def get_editor(editor_id: str) -> dict:
    """Get editor."""
    return {"id": editor_id}
`, models.KindClaudeSDKTool)
	expr := rules.CallUsesParamExpr{
		Callees: []string{"open"},
	}
	if rules.PredCallUsesParam(expr, tool, pf) {
		t.Error("expected CallUsesParam false: 'editor_id' is not path-like")
	}
}
```

- [ ] **Step 5: Run all predicate tests**

```
go test ./internal/rules/... -run "TestPred_" -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```
git add internal/rules/predicates.go internal/rules/predicates_test.go
git commit -m "feat(rules): add all detection predicates"
```

---

## Task 4: Evaluator

**Files:**
- Create: `internal/rules/evaluator.go`
- Create: `internal/rules/evaluator_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/evaluator_test.go`:

```go
package rules_test

import (
	"testing"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/rules"
)

func boolPtr(b bool) *bool { return &b }

// ─── all combinator ───────────────────────────────────────────────────────────

func TestEvaluate_All_BothTrue(t *testing.T) {
	tool := models.ToolDef{Name: "create_order", ParamNames: []string{"customer_id"}}
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{HasParams: boolPtr(true)},
			{NameHasPrefix: []string{"create_"}},
		},
	}
	if !rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected true: both conditions met")
	}
}

func TestEvaluate_All_ShortCircuitsOnFalse(t *testing.T) {
	// First sub-expr is false (no params) → whole all is false without evaluating second.
	tool := models.ToolDef{Name: "create_order", ParamNames: []string{}}
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{HasParams: boolPtr(true)},    // false
			{NameHasPrefix: []string{"create_"}}, // would be true, never reached
		},
	}
	if rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected false: first condition fails")
	}
}

// ─── any combinator ───────────────────────────────────────────────────────────

func TestEvaluate_Any_FirstTrue(t *testing.T) {
	tool := models.ToolDef{Name: "process"}
	expr := rules.MatchExpr{
		Any: []rules.MatchExpr{
			{NameIn: []string{"process"}},    // true
			{HasParams: boolPtr(true)},       // would be false (no params), but not reached
		},
	}
	if !rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected true: first any condition met")
	}
}

func TestEvaluate_Any_AllFalse(t *testing.T) {
	tool := models.ToolDef{Name: "summarize_invoice"}
	expr := rules.MatchExpr{
		Any: []rules.MatchExpr{
			{NameIn: []string{"process", "handle"}},
			{NameHasPrefix: []string{"create_"}},
		},
	}
	if rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected false: no any condition met")
	}
}

// ─── not combinator ───────────────────────────────────────────────────────────

func TestEvaluate_Not_InvertsTrue(t *testing.T) {
	// not(name_in=[process]) on a tool named "process" → false
	tool := models.ToolDef{Name: "process"}
	expr := rules.MatchExpr{
		Not: &rules.MatchExpr{NameIn: []string{"process"}},
	}
	if rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected false: not(true) = false")
	}
}

func TestEvaluate_Not_InvertsFalse(t *testing.T) {
	// not(name_in=[process]) on a tool named "summarize_invoice" → true
	tool := models.ToolDef{Name: "summarize_invoice"}
	expr := rules.MatchExpr{
		Not: &rules.MatchExpr{NameIn: []string{"process"}},
	}
	if !rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected true: not(false) = true")
	}
}

// ─── nested combinators ───────────────────────────────────────────────────────

func TestEvaluate_Nested_AllWithNot(t *testing.T) {
	// all: [has_params=true, not(has_typed_params=true)] on a tool with untyped params
	tool := models.ToolDef{
		Name:           "foo",
		ParamNames:     []string{"x"},
		HasInputSchema: false,
	}
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{HasParams: boolPtr(true)},
			{Not: &rules.MatchExpr{HasTypedParams: boolPtr(true)}},
		},
	}
	if !rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected true: has params AND not typed")
	}
}

func TestEvaluate_Nested_AllWithNot_FalseWhenTyped(t *testing.T) {
	// Same expr but with typed params → second condition fails
	tool := models.ToolDef{
		Name:           "foo",
		ParamNames:     []string{"x"},
		HasInputSchema: true,
	}
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{HasParams: boolPtr(true)},
			{Not: &rules.MatchExpr{HasTypedParams: boolPtr(true)}},
		},
	}
	if rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected false: not(has_typed_params) fails when params are typed")
	}
}

// ─── always ───────────────────────────────────────────────────────────────────

func TestEvaluate_Always_True(t *testing.T) {
	tool := models.ToolDef{Name: "anything"}
	expr := rules.MatchExpr{Always: boolPtr(true)}
	if !rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected true: always=true")
	}
}

func TestEvaluate_Always_False(t *testing.T) {
	tool := models.ToolDef{Name: "anything"}
	expr := rules.MatchExpr{Always: boolPtr(false)}
	if rules.Evaluate(expr, tool, analysis.ParsedFile{}) {
		t.Error("expected false: always=false")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/rules/... -run "TestEvaluate_"
```

Expected: compile error — `rules.Evaluate` undefined.

- [ ] **Step 3: Create `internal/rules/evaluator.go`**

```go
package rules

import (
	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/models"
)

// Evaluate returns true if expr matches the given tool and parsed file.
// All set fields at a given MatchExpr node are ANDed (implicit conjunction).
func Evaluate(expr MatchExpr, tool models.ToolDef, pf analysis.ParsedFile) bool {
	// ── combinators ────────────────────────────────────────────────────────────
	if len(expr.All) > 0 {
		for _, sub := range expr.All {
			if !Evaluate(sub, tool, pf) {
				return false
			}
		}
	}
	if len(expr.Any) > 0 {
		anyPassed := false
		for _, sub := range expr.Any {
			if Evaluate(sub, tool, pf) {
				anyPassed = true
				break
			}
		}
		if !anyPassed {
			return false
		}
	}
	if expr.Not != nil {
		if Evaluate(*expr.Not, tool, pf) {
			return false
		}
	}

	// ── leaf predicates (all set predicates must pass) ─────────────────────────
	if expr.Always != nil && !*expr.Always {
		return false
	}
	if expr.HasDocstring != nil && PredHasDocstring(tool) != *expr.HasDocstring {
		return false
	}
	if expr.HasParams != nil && PredHasParams(tool) != *expr.HasParams {
		return false
	}
	if expr.HasTypedParams != nil && PredHasTypedParams(tool) != *expr.HasTypedParams {
		return false
	}
	if expr.HasRaise != nil && PredHasRaise(tool, pf) != *expr.HasRaise {
		return false
	}
	if expr.HasTryExcept != nil && PredHasTryExcept(tool, pf) != *expr.HasTryExcept {
		return false
	}
	if expr.HasShellCall != nil && PredHasShellCall(tool, pf) != *expr.HasShellCall {
		return false
	}
	if expr.HasWriteCall != nil && PredHasWriteCall(tool, pf) != *expr.HasWriteCall {
		return false
	}
	if expr.HasDynamicURLCall != nil && PredHasDynamicURLCall(tool, pf) != *expr.HasDynamicURLCall {
		return false
	}
	if len(expr.NameIn) > 0 && !PredNameIn(expr.NameIn, tool) {
		return false
	}
	if len(expr.NameHasPrefix) > 0 && !PredNameHasPrefix(expr.NameHasPrefix, tool) {
		return false
	}
	if len(expr.HasBodyText) > 0 && !PredHasBodyText(expr.HasBodyText, tool, pf) {
		return false
	}
	if expr.ParamNameMatches != nil && !PredParamNameMatches(*expr.ParamNameMatches, tool) {
		return false
	}
	if expr.CallWithoutKwarg != nil && !PredCallWithoutKwarg(*expr.CallWithoutKwarg, tool, pf) {
		return false
	}
	if expr.CallWithKwargValue != nil && !PredCallWithKwargValue(*expr.CallWithKwargValue, tool, pf) {
		return false
	}
	if expr.CallUsesParam != nil && !PredCallUsesParam(*expr.CallUsesParam, tool, pf) {
		return false
	}
	return true
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/rules/... -run "TestEvaluate_" -v
```

Expected: all 9 tests PASS.

- [ ] **Step 5: Commit**

```
git add internal/rules/evaluator.go internal/rules/evaluator_test.go
git commit -m "feat(rules): add recursive evaluator for match expressions"
```

---

## Task 5: YAML policy files

**Files:**
- Create: `internal/rules/policies/claude_sdk.yaml`
- Create: `internal/rules/policies/openshell.yaml`

- [ ] **Step 1: Create `internal/rules/policies/claude_sdk.yaml`**

```yaml
policy:
  id: claude_sdk
  name: Claude Agent SDK Reliability
  category: claude_sdk
  description: |
    Reliability checks for tool functions decorated with the Claude Agent SDK.

rules:
  - id: CSDK-001
    title: Tool has no description
    severity: low
    confidence: 0.95
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      not:
        has_docstring: true
    explanation: |
      The Claude Agent SDK uses the tool's docstring as the description shown
      to the model. With no description, the model must guess from the function
      name when to call this tool — which causes mis-selection under ambiguous
      prompts.
    fix: Add a one-paragraph docstring describing inputs, outputs, and when to use this tool.
    fix_hints:
      add_docstring: true

  - id: CSDK-002
    title: Tool parameters are not type-annotated
    severity: medium
    confidence: 0.9
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      all:
        - has_params: true
        - not:
            has_typed_params: true
    explanation: |
      Without parameter type annotations, the SDK cannot generate an input schema
      to validate model output. The model can hallucinate parameter shapes the tool
      does not accept, and the failure surfaces at runtime as a TypeError instead
      of a clean validation error pre-invocation.
    fix: Annotate every parameter with a type. Prefer pydantic models for nested args.
    fix_hints:
      add_input_schema: true

  - id: CSDK-003
    title: Network call has no timeout
    severity: high
    confidence: 0.85
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      call_without_kwarg:
        callees:
          - requests.get
          - requests.post
          - requests.put
          - requests.delete
          - requests.patch
          - requests.head
          - requests.request
          - requests.Session.get
          - requests.Session.post
          - httpx.get
          - httpx.post
          - httpx.put
          - httpx.delete
          - httpx.patch
          - httpx.head
          - httpx.request
          - httpx.AsyncClient
          - httpx.Client
          - urllib.request.urlopen
          - aiohttp.ClientSession.get
          - aiohttp.ClientSession.post
        missing: timeout
    explanation: |
      An agent tool that makes a network request without a timeout can hang
      indefinitely, blocking the conversation loop and exhausting the agent's
      wall-clock budget. The SDK does not enforce timeouts for you.
    fix: "Pass `timeout=` (typically 5–30 s) to the request. Surface failures as a structured error the model can react to."
    fix_hints:
      hook: pretooluse_validate
      guard: timeout_required

  - id: CSDK-004
    title: Path parameter used in I/O without validation
    severity: high
    confidence: 0.7
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      all:
        - param_name_matches:
            exact: [path, file, filename, filepath, dir, directory]
            suffixes: [_path, _file, _dir, _directory]
            prefixes: [file_, path_]
        - call_uses_param:
            callees: [open, Path]
            callee_prefixes: [os., shutil.]
        - not:
            has_body_text: [".resolve(", "realpath(", "is_safe_path"]
    explanation: |
      The tool accepts a path-like parameter and passes it to file or directory
      operations without resolving or sandboxing the path. A model-supplied
      ../../etc/passwd is reachable. Detection is heuristic — confirm the
      parameter is genuinely user-supplied before applying the fix.
    fix: "Resolve the path with `Path(...).resolve()` and assert it sits under an allowed root."
    fix_hints:
      hook: pretooluse_validate
      guard: path_under_root

  - id: CSDK-005
    title: Tool raises exceptions without a structured error contract
    severity: medium
    confidence: 0.6
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      all:
        - has_raise: true
        - not:
            has_try_except: true
    explanation: |
      When a tool raises, the SDK surfaces the exception to the model as an opaque
      string. The model often cannot recover or retry intelligently. Returning a
      structured error object lets the model branch on the failure mode.
    fix: "Catch known failure modes and return a `{\"error\": ..., \"retryable\": bool}` payload."
    fix_hints:
      wrap_errors: true

  - id: CSDK-006
    title: Mutating tool has no idempotency key
    severity: medium
    confidence: 0.55
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      all:
        - name_has_prefix: [create_, send_, delete_, post_, update_, refund_, charge_, issue_]
        - not:
            param_name_matches:
              exact: [idempotency_key, request_id, txn_id]
              contains: [idempot]
    explanation: |
      Tool name suggests a side effect (create/send/refund/…). Agents retry tool
      calls under timeouts and ambiguous failures; without an idempotency key the
      same action can fire twice. The hook generator can stamp a key into the
      request, but the downstream service has to honor it.
    fix: "Add an `idempotency_key: str` parameter; verify the receiving API respects it."
    fix_hints:
      hook: pretooluse_validate
      inject_idempotency: true

  - id: CSDK-007
    title: Ambiguous tool name
    severity: low
    confidence: 0.9
    applies_to:
      - claude_sdk_tool
      - mcp_tool
    match:
      name_in: [process, handle, run, do, execute, perform, work, go, thing, stuff]
    explanation: |
      Tool names like `process`, `handle`, or `run` give the model no signal about
      intent. The model will either call this tool for the wrong job or refuse to
      call it at all.
    fix: "Rename to a verb-object form, e.g. `summarize_invoice`, `refund_charge`."
```

- [ ] **Step 2: Create `internal/rules/policies/openshell.yaml`**

```yaml
policy:
  id: openshell
  name: OpenShell Sandbox Policies
  category: openshell
  description: |
    Security checks that drive OpenShell sandbox policy generation.

rules:
  - id: OSH-001
    title: subprocess called with shell=True
    severity: critical
    confidence: 0.99
    applies_to:
      - claude_sdk_tool
      - mcp_tool
      - shell_invocation
    match:
      call_with_kwarg_value:
        callee_prefix: "subprocess."
        kwarg: shell
        value: "True"
    explanation: |
      `shell=True` invokes the system shell to interpret the command string, which
      means any model-controlled substring becomes a shell injection vector. The
      OpenShell sandbox cannot fully constrain this — moving to a list-form
      invocation is required, not optional.
    fix: "Pass the command as a list and remove `shell=True`. Encode any user data as a positional argument, never via string interpolation."
    fix_hints:
      policy_action: deny_shell_true

  - id: OSH-002
    title: Shell invocation without an allowed-command list
    severity: high
    confidence: 0.85
    applies_to:
      - claude_sdk_tool
      - mcp_tool
      - shell_invocation
    match:
      all:
        - has_shell_call: true
        - not:
            has_body_text: [ALLOWED_COMMANDS, allowlist, ALLOWED_CMDS]
    explanation: |
      The tool shells out but does not constrain which binaries can be invoked.
      Even with `shell=False`, an unconstrained `argv[0]` lets the model invoke
      arbitrary installed binaries. OpenShell can enforce an allowlist at the
      sandbox layer.
    fix: "Define a constant ALLOWED_COMMANDS and assert argv[0] is in it. The generated OpenShell policy will also include a command allowlist."
    fix_hints:
      policy_emit: command_allowlist

  - id: OSH-003
    title: Filesystem write without sandbox restriction
    severity: high
    confidence: 0.8
    applies_to:
      - claude_sdk_tool
      - mcp_tool
      - shell_invocation
    match:
      has_write_call: true
    explanation: |
      The tool writes to or deletes from the filesystem. Without an OpenShell
      write-prefix restriction, a path-traversal in the input lets the agent
      clobber files outside its working directory.
    fix: "Constrain writes to a single working directory and let the generated OpenShell policy enforce the prefix at the sandbox layer."
    fix_hints:
      policy_emit: fs_write_prefix

  - id: OSH-004
    title: No OpenShell resource limits configured
    severity: medium
    confidence: 0.95
    applies_to:
      - claude_sdk_tool
      - mcp_tool
      - shell_invocation
    singleton: true
    match:
      always: true
    explanation: |
      The repo declares no OpenShell policy with cpu/memory/time limits. A runaway
      tool call (infinite loop, memory blow-up) is unbounded by default.
    fix: "The generated openshell/policy.yaml will include default limits — review them against your expected workloads before committing."
    fix_hints:
      policy_emit: default_resource_limits

  - id: OSH-005
    title: Network egress is unrestricted
    severity: high
    confidence: 0.7
    applies_to:
      - claude_sdk_tool
      - mcp_tool
      - shell_invocation
    match:
      all:
        - has_dynamic_url_call: true
        - not:
            has_body_text: [ALLOWED_HOSTS, allowed_hosts]
    explanation: |
      The tool issues HTTP calls to a URL derived from inputs, with no host
      allowlist in the code path. OpenShell can restrict outbound DNS/connect at
      the sandbox layer, which is strictly more reliable than enforcing it in
      Python.
    fix: "Define an allowlist of hostnames the tool may reach. The generated OpenShell policy will gate egress to those hosts."
    fix_hints:
      policy_emit: network_allowlist
```

- [ ] **Step 3: Validate files parse cleanly**

```
go run -v ./... 2>&1 | head -5
```

(No test here — just confirm `go build ./...` succeeds after Task 6 adds the embed.)

- [ ] **Step 4: Commit**

```
git add internal/rules/policies/
git commit -m "feat(rules): add YAML policy files for CSDK and OpenShell rules"
```

---

## Task 6: Embed

**Files:**
- Create: `internal/rules/embed.go`

- [ ] **Step 1: Create `internal/rules/embed.go`**

```go
package rules

import (
	"embed"
	"io/fs"
)

//go:embed policies/*.yaml
var defaultFS embed.FS

// DefaultFS is the embedded rule policies shipped with the binary.
// It is rooted at the policies/ subdirectory so Load(DefaultFS) works directly.
var DefaultFS fs.FS = mustSub(defaultFS, "policies")

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // policies/ always exists — only fires if embed is misconfigured
	}
	return sub
}
```

- [ ] **Step 2: Confirm build succeeds**

```
go build ./internal/rules/...
```

Expected: no errors.

- [ ] **Step 3: Write a smoke test that DefaultFS loads the embedded policies**

Add to `internal/rules/loader_test.go`:

```go
func TestLoader_DefaultFS_LoadsCleanly(t *testing.T) {
	policies, err := rules.Load(rules.DefaultFS)
	if err != nil {
		t.Fatalf("Load(DefaultFS): %v", err)
	}
	var ruleCount int
	for _, p := range policies {
		ruleCount += len(p.Rules)
	}
	if ruleCount != 12 {
		t.Errorf("expected 12 rules from DefaultFS, got %d", ruleCount)
	}
}
```

- [ ] **Step 4: Run the smoke test**

```
go test ./internal/rules/... -run TestLoader_DefaultFS_LoadsCleanly -v
```

Expected: PASS (12 rules: CSDK-001..007 + OSH-001..005).

- [ ] **Step 5: Commit**

```
git add internal/rules/embed.go internal/rules/loader_test.go
git commit -m "feat(rules): embed policy YAML files as DefaultFS"
```

---

## Task 7: Policies integration test

**Files:**
- Create: `internal/rules/policies_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/policies_test.go`:

```go
package rules_test

import (
	"testing"

	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/rules"
)

// loadRule finds a rule by ID from DefaultFS. Fails fast if not found.
func loadRule(t *testing.T, id string) rules.RuleDef {
	t.Helper()
	policies, err := rules.Load(rules.DefaultFS)
	if err != nil {
		t.Fatalf("Load(DefaultFS): %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == id {
				return r
			}
		}
	}
	t.Fatalf("rule %q not found in DefaultFS", id)
	return rules.RuleDef{}
}

// ─── CSDK-001 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK001_Fires_NoDocstring(t *testing.T) {
	rule := loadRule(t, "CSDK-001")
	tool, pf := parsePy(t, `
def fetch_data(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-001 should fire on tool with no docstring")
	}
}

func TestPolicy_CSDK001_Silent_HasDocstring(t *testing.T) {
	rule := loadRule(t, "CSDK-001")
	tool, pf := parsePy(t, `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-001 should NOT fire when tool has a docstring")
	}
}

// ─── CSDK-002 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK002_Fires_UntypedParams(t *testing.T) {
	rule := loadRule(t, "CSDK-002")
	tool, pf := parsePy(t, `
def fetch_data(x, y):
    """Does something."""
    return {}
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-002 should fire on untyped params")
	}
}

func TestPolicy_CSDK002_Silent_TypedParams(t *testing.T) {
	rule := loadRule(t, "CSDK-002")
	tool, pf := parsePy(t, `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-002 should NOT fire when params are typed")
	}
}

// ─── CSDK-003 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK003_Fires_NoTimeout(t *testing.T) {
	rule := loadRule(t, "CSDK-003")
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id).json()
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-003 should fire when no timeout")
	}
}

func TestPolicy_CSDK003_Silent_HasTimeout(t *testing.T) {
	rule := loadRule(t, "CSDK-003")
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id, timeout=10).json()
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-003 should NOT fire when timeout is present")
	}
}

// ─── CSDK-004 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK004_Fires_PathInOpen(t *testing.T) {
	rule := loadRule(t, "CSDK-004")
	tool, pf := parsePy(t, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-004 should fire on path param in open()")
	}
}

func TestPolicy_CSDK004_Silent_WithResolve(t *testing.T) {
	rule := loadRule(t, "CSDK-004")
	tool, pf := parsePy(t, `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-004 should NOT fire when .resolve() is present")
	}
}

// ─── CSDK-005 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK005_Fires_RawRaise(t *testing.T) {
	rule := loadRule(t, "CSDK-005")
	tool, pf := parsePy(t, `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-005 should fire on raw raise with no try/except")
	}
}

func TestPolicy_CSDK005_Silent_HasTryExcept(t *testing.T) {
	rule := loadRule(t, "CSDK-005")
	tool, pf := parsePy(t, `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-005 should NOT fire when raise is inside try/except")
	}
}

// ─── CSDK-006 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK006_Fires_MutatingNoIdempotency(t *testing.T) {
	rule := loadRule(t, "CSDK-006")
	tool, pf := parsePy(t, `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-006 should fire on mutating tool with no idempotency key")
	}
}

func TestPolicy_CSDK006_Silent_HasIdempotencyKey(t *testing.T) {
	rule := loadRule(t, "CSDK-006")
	tool, pf := parsePy(t, `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-006 should NOT fire when idempotency_key param is present")
	}
}

// ─── CSDK-007 ─────────────────────────────────────────────────────────────────

func TestPolicy_CSDK007_Fires_AmbiguousName(t *testing.T) {
	rule := loadRule(t, "CSDK-007")
	tool, pf := parsePy(t, `
def process(data: dict) -> dict:
    """Process data."""
    return data
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-007 should fire on ambiguous name 'process'")
	}
}

func TestPolicy_CSDK007_Silent_DescriptiveName(t *testing.T) {
	rule := loadRule(t, "CSDK-007")
	tool, pf := parsePy(t, `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("CSDK-007 should NOT fire on descriptive name")
	}
}

// ─── OSH-001 ──────────────────────────────────────────────────────────────────

func TestPolicy_OSH001_Fires_ShellTrue(t *testing.T) {
	rule := loadRule(t, "OSH-001")
	tool, pf := parsePy(t, `
import subprocess
def run_report(name: str) -> str:
    """Run report."""
    subprocess.run(f"report-tool {name}", shell=True)
    return "done"
`, models.KindShellInvocation)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-001 should fire on shell=True")
	}
}

func TestPolicy_OSH001_Silent_ListForm(t *testing.T) {
	rule := loadRule(t, "OSH-001")
	tool, pf := parsePy(t, `
import subprocess
def run_report(name: str) -> str:
    """Run report."""
    subprocess.run(["report-tool", name])
    return "done"
`, models.KindShellInvocation)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-001 should NOT fire on list-form call")
	}
}

// ─── OSH-002 ──────────────────────────────────────────────────────────────────

func TestPolicy_OSH002_Fires_NoAllowlist(t *testing.T) {
	rule := loadRule(t, "OSH-002")
	tool, pf := parsePy(t, `
import subprocess
def run_cmd(cmd: str) -> str:
    """Run command."""
    subprocess.run([cmd])
    return "done"
`, models.KindShellInvocation)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-002 should fire when no ALLOWED_COMMANDS")
	}
}

func TestPolicy_OSH002_Silent_HasAllowlist(t *testing.T) {
	rule := loadRule(t, "OSH-002")
	tool, pf := parsePy(t, `
import subprocess
ALLOWED_COMMANDS = ["git", "python3"]
def run_cmd(cmd: str) -> str:
    """Run an allowed command."""
    assert cmd in ALLOWED_COMMANDS
    subprocess.run([cmd])
    return "done"
`, models.KindShellInvocation)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-002 should NOT fire when ALLOWED_COMMANDS is present")
	}
}

// ─── OSH-003 ──────────────────────────────────────────────────────────────────

func TestPolicy_OSH003_Fires_OpenWrite(t *testing.T) {
	rule := loadRule(t, "OSH-003")
	tool, pf := parsePy(t, `
def write_output(name: str) -> str:
    """Write output."""
    with open(f"/tmp/{name}.txt", "w") as f:
        f.write("data")
    return "done"
`, models.KindShellInvocation)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-003 should fire on open() write mode")
	}
}

func TestPolicy_OSH003_Silent_ReadOnly(t *testing.T) {
	rule := loadRule(t, "OSH-003")
	tool, pf := parsePy(t, `
def read_output(name: str) -> str:
    """Read output."""
    with open(f"/tmp/{name}.txt", "r") as f:
        return f.read()
`, models.KindShellInvocation)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-003 should NOT fire on read-only open()")
	}
}

// ─── OSH-004 ──────────────────────────────────────────────────────────────────

func TestPolicy_OSH004_Fires_AnyTool(t *testing.T) {
	rule := loadRule(t, "OSH-004")
	tool, pf := parsePy(t, `
def some_tool(x: str) -> str:
    """Does something."""
    return x
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-004 should fire unconditionally (always: true)")
	}
}

// ─── OSH-005 ──────────────────────────────────────────────────────────────────

func TestPolicy_OSH005_Fires_DynamicURL(t *testing.T) {
	rule := loadRule(t, "OSH-005")
	tool, pf := parsePy(t, `
import requests
def fetch_resource(url: str) -> dict:
    """Fetch from a dynamic URL."""
    return requests.get(url).json()
`, models.KindClaudeSDKTool)
	if !rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-005 should fire on dynamic URL")
	}
}

func TestPolicy_OSH005_Silent_LiteralURL(t *testing.T) {
	rule := loadRule(t, "OSH-005")
	tool, pf := parsePy(t, `
import requests
def fetch_resource() -> dict:
    """Fetch from a known endpoint."""
    return requests.get("https://api.example.com/data").json()
`, models.KindClaudeSDKTool)
	if rules.Evaluate(rule.Match, tool, pf) {
		t.Error("OSH-005 should NOT fire on literal URL")
	}
}
```

- [ ] **Step 2: Run all policy tests**

```
go test ./internal/rules/... -run "TestPolicy_" -v
```

Expected: all 24 tests PASS.

- [ ] **Step 3: Run all rules package tests**

```
go test ./internal/rules/... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```
git add internal/rules/policies_test.go
git commit -m "test(rules): add per-rule integration tests against DefaultFS"
```

---

## Task 8: RuleDetector and LoadRegistry

**Files:**
- Modify: `internal/analysis/detectors/detector.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/analysis/detectors/detector.go`'s test file. Create `internal/analysis/detectors/load_registry_test.go`:

```go
package detectors_test

import (
	"testing"

	"github.com/trustabl/karenctl/internal/analysis/detectors"
	"github.com/trustabl/karenctl/internal/rules"
)

func TestLoadRegistry_DefaultFS_Returns12Detectors(t *testing.T) {
	reg, err := detectors.LoadRegistry(rules.DefaultFS)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reg.Count() != 12 {
		t.Errorf("expected 12 detectors, got %d", reg.Count())
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/analysis/detectors/... -run TestLoadRegistry_DefaultFS_Returns12Detectors
```

Expected: compile error — `detectors.LoadRegistry` undefined.

- [ ] **Step 3: Add `RuleDetector` and `LoadRegistry` to `internal/analysis/detectors/detector.go`**

Add these declarations after the existing `Registry` struct definition. Do NOT remove any existing code yet.

```go
import (
    "fmt"
    "io/fs"

    "github.com/trustabl/karenctl/internal/rules"
)
```

Append to `internal/analysis/detectors/detector.go`:

```go
// RuleDetector wraps a single RuleDef and implements the Detector interface.
type RuleDetector struct {
	def rules.RuleDef
}

func (d RuleDetector) RuleID() string                    { return d.def.ID }
func (d RuleDetector) Category() models.DetectorCategory { return d.def.Category }
func (d RuleDetector) Singleton() bool                   { return d.def.Singleton }
func (d RuleDetector) Applies(t models.ToolDef) bool {
	for _, k := range d.def.AppliesTo {
		if string(t.Kind) == k {
			return true
		}
	}
	return false
}
func (d RuleDetector) Detect(t models.ToolDef, pf analysis.ParsedFile) []models.Finding {
	if !rules.Evaluate(d.def.Match, t, pf) {
		return nil
	}
	return []models.Finding{{
		RuleID:       d.def.ID,
		Category:     d.def.Category,
		Severity:     d.def.Severity,
		ToolName:     t.Name,
		FilePath:     t.FilePath,
		Line:         t.Line,
		Title:        d.def.Title,
		Explanation:  d.def.Explanation,
		SuggestedFix: d.def.Fix,
		Confidence:   d.def.Confidence,
		FixHints:     d.def.FixHints,
	}}
}

// LoadRegistry loads all YAML policy files from fsys and returns a Registry.
// Pass rules.DefaultFS for the embedded policies or os.DirFS for a custom set.
func LoadRegistry(fsys fs.FS) (*Registry, error) {
	policies, err := rules.Load(fsys)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	var dets []Detector
	for _, pf := range policies {
		for _, def := range pf.Rules {
			dets = append(dets, RuleDetector{def: def})
		}
	}
	return &Registry{detectors: dets}, nil
}
```

- [ ] **Step 4: Run test**

```
go test ./internal/analysis/detectors/... -run TestLoadRegistry_DefaultFS_Returns12Detectors -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/analysis/detectors/detector.go internal/analysis/detectors/load_registry_test.go
git commit -m "feat(detectors): add RuleDetector and LoadRegistry from YAML policies"
```

---

## Task 9: Parallel Registry.Run

**Files:**
- Modify: `internal/analysis/detectors/detector.go`

- [ ] **Step 1: Write a test for deterministic parallel output**

Add to `internal/analysis/detectors/load_registry_test.go`:

```go
import (
    "sort"

    "github.com/trustabl/karenctl/internal/analysis"
    "github.com/trustabl/karenctl/internal/models"
)

func TestRegistry_Run_Deterministic(t *testing.T) {
	reg, err := detectors.LoadRegistry(rules.DefaultFS)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	// A tool that triggers CSDK-001 (no docstring) and CSDK-007 (name=process).
	tool := models.ToolDef{
		Name:     "process",
		Kind:     models.KindClaudeSDKTool,
		FilePath: "test.py",
		Line:     1,
		Facts:    map[string]string{},
	}
	pf := analysis.ParsedFile{RelPath: "test.py"}

	run1 := reg.Run([]models.ToolDef{tool}, []analysis.ParsedFile{pf})
	run2 := reg.Run([]models.ToolDef{tool}, []analysis.ParsedFile{pf})

	if len(run1) != len(run2) {
		t.Fatalf("non-deterministic finding count: %d vs %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i].RuleID != run2[i].RuleID {
			t.Errorf("finding[%d] differs: %s vs %s", i, run1[i].RuleID, run2[i].RuleID)
		}
	}
	// Verify output is sorted by RuleID.
	ids := make([]string, len(run1))
	for i, f := range run1 {
		ids[i] = f.RuleID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("findings are not sorted by RuleID: %v", ids)
	}
}
```

- [ ] **Step 2: Run test (should pass with existing sequential Run, establishing the contract)**

```
go test ./internal/analysis/detectors/... -run TestRegistry_Run_Deterministic -v
```

Expected: PASS (sequential Run is already deterministic).

- [ ] **Step 3: Replace sequential `Registry.Run` with a parallel worker pool**

Replace the existing `Run` method in `internal/analysis/detectors/detector.go`:

```go
import (
    "runtime"
    "sort"
    "sync"
)

// Run executes every applicable detector against every tool in parallel,
// using a bounded worker pool. Output is sorted by (RuleID, FilePath, Line)
// for deterministic results. Singleton rules fire at most once per scan.
func (r *Registry) Run(tools []models.ToolDef, files []analysis.ParsedFile) []models.Finding {
	byPath := make(map[string]analysis.ParsedFile, len(files))
	for _, f := range files {
		byPath[f.RelPath] = f
	}

	type workItem struct {
		detector Detector
		tool     models.ToolDef
		pf       analysis.ParsedFile
	}

	var items []workItem
	for _, d := range r.detectors {
		for _, t := range tools {
			pf, ok := byPath[t.FilePath]
			if !ok || !d.Applies(t) {
				continue
			}
			items = append(items, workItem{d, t, pf})
		}
	}
	if len(items) == 0 {
		return nil
	}

	workCh := make(chan workItem, len(items))
	for _, item := range items {
		workCh <- item
	}
	close(workCh)

	resultCh := make(chan []models.Finding, len(items))
	n := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				resultCh <- item.detector.Detect(item.tool, item.pf)
			}
		}()
	}
	wg.Wait()
	close(resultCh)

	var all []models.Finding
	for findings := range resultCh {
		all = append(all, findings...)
	}

	// Sort for deterministic output.
	sort.Slice(all, func(i, j int) bool {
		if all[i].RuleID != all[j].RuleID {
			return all[i].RuleID < all[j].RuleID
		}
		if all[i].FilePath != all[j].FilePath {
			return all[i].FilePath < all[j].FilePath
		}
		return all[i].Line < all[j].Line
	})

	// Dedup singleton rules: keep only the first finding per singleton rule ID.
	singletonRules := make(map[string]bool)
	for _, d := range r.detectors {
		if d.Singleton() {
			singletonRules[d.RuleID()] = true
		}
	}
	fired := make(map[string]bool)
	out := all[:0]
	for _, f := range all {
		if singletonRules[f.RuleID] {
			if fired[f.RuleID] {
				continue
			}
			fired[f.RuleID] = true
		}
		out = append(out, f)
	}
	return out
}
```

- [ ] **Step 4: Run all detectors tests**

```
go test ./internal/analysis/detectors/... -v -race
```

Expected: all tests PASS. The `-race` flag confirms no data races.

- [ ] **Step 5: Commit**

```
git add internal/analysis/detectors/detector.go internal/analysis/detectors/load_registry_test.go
git commit -m "feat(detectors): replace sequential Run with bounded parallel worker pool"
```

---

## Task 10: Scanner and CLI wiring

**Files:**
- Modify: `internal/scanner/scanner.go`
- Modify: `cmd/karenctl/main.go`

- [ ] **Step 1: Modify `internal/scanner/scanner.go`**

Add `RulesDir string` to `Config` and change `Run` to call `LoadRegistry`:

```go
import (
    "io/fs"
    "os"

    "github.com/trustabl/karenctl/internal/analysis/detectors"
    "github.com/trustabl/karenctl/internal/rules"
)

// Config configures one scan. Zero-value is "scan everything, generate everything".
type Config struct {
	Target     string                    // local path or GitHub URL
	Categories []models.DetectorCategory // empty means all categories
	Version    string                    // injected by the CLI for artifact metadata
	RulesDir   string                    // empty = use embedded default rules
}
```

Replace the `detectors.NewRegistry()` block in `Run`:

```go
	var fsys fs.FS = rules.DefaultFS
	if cfg.RulesDir != "" {
		fsys = os.DirFS(cfg.RulesDir)
	}
	registry, err := detectors.LoadRegistry(fsys)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("rules: %w", err)
	}
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
	}
```

Remove the old lines:
```go
	registry := detectors.NewRegistry()
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
	}
```

- [ ] **Step 2: Add `--rules-dir` flag to `cmd/karenctl/main.go`**

In `scanFlags`:
```go
type scanFlags struct {
	detectors string
	format    string
	apply     bool
	export    string
	yes       bool
	overwrite bool
	strict    bool
	noColor   bool
	rulesDir  string  // new field
}
```

In `newScanCommand`, add:
```go
cmd.Flags().StringVar(&f.rulesDir, "rules-dir", "",
    "load detection rules from this directory instead of the embedded defaults")
```

In `runScan`, add `RulesDir`:
```go
cfg := scanner.Config{Target: target, Version: version, RulesDir: f.rulesDir}
```

- [ ] **Step 3: Run the end-to-end scanner test (the migration correctness gate)**

```
go test ./internal/scanner/... -v -run TestScanSampleAgent
```

Expected: PASS. All 12 rules fire on `examples/sample_agent/`, both artifact files present, deterministic output.

If it fails, debug the YAML rules (most likely a predicate or evaluator edge case). The scanner test is the correctness gate — do not proceed to Task 11 until it passes.

- [ ] **Step 4: Run full test suite**

```
go test ./... -race
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```
git add internal/scanner/scanner.go cmd/karenctl/main.go
git commit -m "feat(scanner): switch to YAML-based LoadRegistry; add --rules-dir flag"
```

---

## Task 11: Delete old Go detectors

This task is the cleanup step. The scanner end-to-end test passed in Task 10, confirming the YAML port is complete.

**Files:**
- Delete: `internal/analysis/detectors/claude_sdk.go`
- Delete: `internal/analysis/detectors/openshell.go`
- Delete: `internal/analysis/detectors/detectors_test.go`
- Modify: `internal/analysis/detectors/detector.go` (remove dead code)

- [ ] **Step 1: Delete the old Go detector implementations**

```
git rm internal/analysis/detectors/claude_sdk.go
git rm internal/analysis/detectors/openshell.go
git rm internal/analysis/detectors/detectors_test.go
```

- [ ] **Step 2: Remove dead exports and types from `detector.go`**

Remove from `internal/analysis/detectors/detector.go`:

- `NewRegistry()` function (replaced by `LoadRegistry`)
- All exported constructor functions: `CSDK001()` through `OSH005()`
- The `notSingleton` embed struct and its `Singleton()` method

The file should retain: `Detector` interface, `Registry` struct, `Subset`, `Run`, `Count`, `RuleDetector`, `LoadRegistry`.

- [ ] **Step 3: Confirm the build compiles cleanly**

```
go build ./...
```

Expected: no errors. If there are "undefined" errors, the deleted functions are referenced somewhere — fix those references.

- [ ] **Step 4: Run the full test suite one final time**

```
go test ./... -race -v 2>&1 | tail -40
```

Expected: all tests PASS. Key checks:
- `TestScanSampleAgent` — all 12 rules fire, 3 artifact files present, deterministic
- `TestPolicy_*` — all 24 policy integration tests pass
- `TestEvaluate_*` — all evaluator combinator tests pass
- `TestPred_*` — all predicate tests pass
- `TestLoader_*` — all loader tests pass
- `TestRegistry_Run_Deterministic` — parallel output is sorted and stable

- [ ] **Step 5: Commit**

```
git add internal/analysis/detectors/detector.go
git commit -m "chore(detectors): delete Go detector structs; rules now defined in YAML"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task that implements it |
|---|---|
| YAML schema — PolicyFile, RuleDef, MatchExpr | Task 1 |
| Loader validates unknown predicates via KnownFields(true) | Task 2 |
| Loader validates required fields, duplicate IDs | Task 2 |
| All 16 predicates + 3 combinators | Tasks 3–4 |
| Recursive evaluator with short-circuit | Task 4 |
| go:embed DefaultFS | Task 6 |
| YAML files for all 12 rules | Task 5 |
| Config.RulesDir override | Task 10 |
| RuleDetector wraps RuleDef | Task 8 |
| LoadRegistry(fs.FS) | Task 8 |
| Parallel worker pool, singleton dedup, sorted output | Task 9 |
| scanner_test.go unchanged and passes | Task 10 |
| Old Go structs deleted | Task 11 |

**Type consistency check:**

- `rules.Evaluate` signature: `(MatchExpr, models.ToolDef, analysis.ParsedFile) bool` — used in Task 4 (evaluator.go), Task 7 (policies_test.go), Task 8 (RuleDetector.Detect) ✓
- `rules.Load` signature: `(fs.FS) ([]PolicyFile, error)` — used in Task 2 (loader_test.go), Task 6 (embed smoke test), Task 7 (loadRule helper), Task 8 (LoadRegistry) ✓
- `detectors.LoadRegistry` signature: `(fs.FS) (*Registry, error)` — used in Task 8 (load_registry_test.go), Task 10 (scanner.go) ✓
- `RuleDef.Category` is `models.DetectorCategory` set by loader — `RuleDetector.Category()` returns it correctly ✓
- `RuleDef.Severity` is `models.Severity` — unmarshals from string like `"high"` directly ✓
