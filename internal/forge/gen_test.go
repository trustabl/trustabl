package forge

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

var update = flag.Bool("update", false, "update golden files")

func TestSeverityRank_Order(t *testing.T) {
	order := []models.Severity{
		models.SeverityCritical,
		models.SeverityHigh,
		models.SeverityMedium,
		models.SeverityLow,
	}
	for i := 1; i < len(order); i++ {
		if severityRank(order[i-1]) >= severityRank(order[i]) {
			t.Errorf("severityRank(%q) >= severityRank(%q), want strictly less",
				order[i-1], order[i])
		}
	}
}

func TestSeverityRank_Unknown(t *testing.T) {
	if severityRank("bogus") != 4 {
		t.Error("unknown severity should return 4")
	}
}

func TestFirstSentence_Basic(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"Hello world. Second sentence.", 0, "Hello world."},
		{"No period here", 0, "No period here"},
		{"A very long sentence that exceeds the limit.", 10, "A very…"},
		{"Héllo wörld. Next.", 5, "Héll…"},
		{"  Trimmed.  More text.", 0, "Trimmed."},
		{"Folded\nnewline here. Next.", 0, "Folded newline here."},
	}
	for _, c := range cases {
		got := firstSentence(c.input, c.maxLen)
		if got != c.want {
			t.Errorf("firstSentence(%q, %d) = %q, want %q", c.input, c.maxLen, got, c.want)
		}
	}
}

func TestMatchCondition_SkillPredicates(t *testing.T) {
	boolTrue := true
	cases := []struct {
		name string
		expr rules.MatchExpr
		want string // must be non-empty and not the fallback
	}{
		{
			name: "unrestricted shell",
			expr: rules.MatchExpr{SkillAllowsUnrestrictedShell: &boolTrue},
			want: "Every skill — always verify allowed-tools before emitting.",
		},
		{
			name: "dynamic exec",
			expr: rules.MatchExpr{SkillBodyHasDynamicExec: &boolTrue},
			want: "Any skill body that uses the backtick-exec or fenced-exec form.",
		},
		{
			name: "missing description",
			expr: rules.MatchExpr{SkillHasDescription: &boolTrue},
			want: "Every skill — description is required.",
		},
		{
			name: "agent specific",
			expr: rules.MatchExpr{SkillIsAgentSpecific: &boolTrue},
			want: "Only when frontmatter includes a context: or agent: binding field.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchCondition(c.expr)
			if got != c.want {
				t.Errorf("matchCondition() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMatchCondition_Compound_All(t *testing.T) {
	boolTrue := true
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{SkillModelInvocable: &boolTrue},
			{SkillAllowsTool: []string{"Bash", "Write"}},
		},
	}
	got := matchCondition(expr)
	want := "Any skill where disable-model-invocation is not set to true, and Any skill pre-approving Bash, Write, Edit, WebFetch, or NotebookEdit in allowed-tools."
	if got != want {
		t.Errorf("matchCondition(compound all) = %q, want %q", got, want)
	}
}

func TestMatchCondition_Unknown_Fallback(t *testing.T) {
	got := matchCondition(rules.MatchExpr{})
	want := "When the condition described by this rule is met."
	if got != want {
		t.Errorf("matchCondition({}) = %q, want %q", got, want)
	}
}

func TestGenerate_GoldenFile(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	if len(policies) == 0 {
		t.Fatal("no policies loaded from fixture")
	}
	pf := policies[0]

	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	if len(skillRules) == 0 {
		t.Fatal("no skill-scoped rules found in fixture")
	}

	got := Generate(pf.Policy, skillRules)

	goldenPath := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "expected", "SKILL.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file at %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file not found at %s; run tests with -update to create it:\n  go test ./internal/forge/... -update", goldenPath)
	}
	want := string(wantBytes)
	if got != want {
		t.Errorf("Generate() output does not match golden file %s\n\nrun:\n  go test ./internal/forge/... -update\n\n%s",
			goldenPath, lineDiff(want, got))
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	pf := policies[0]
	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	a := Generate(pf.Policy, skillRules)
	b := Generate(pf.Policy, skillRules)
	if a != b {
		t.Error("Generate is not deterministic: two calls with same input produced different output")
	}

	// prove order-independence: shuffled input must produce identical output
	shuffled := make([]rules.RuleDef, len(skillRules))
	copy(shuffled, skillRules)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	c := Generate(pf.Policy, shuffled)
	if a != c {
		t.Error("Generate is not order-independent: shuffled input produced different output")
	}
}

func TestGenerate_EmptyRules(t *testing.T) {
	meta := rules.PolicyMeta{
		ID:          "test_policy",
		Name:        "Test Policy",
		Description: "A test policy. Second sentence.",
	}
	got := Generate(meta, nil)
	if !strings.Contains(got, "name: test-policy") {
		t.Errorf("expected name: test-policy in output, got:\n%s", got)
	}
	if !strings.Contains(got, "allowed-tools: Read") {
		t.Error("expected allowed-tools: Read in frontmatter")
	}
	if strings.Contains(got, "## [") {
		t.Error("empty rules should produce no constraint blocks")
	}
}

func TestGenerate_SkillCompliant(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	pf := policies[0]
	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	got := Generate(pf.Policy, skillRules)

	// The generated skill must itself be CSKILL-compliant.
	if strings.Contains(got, "allowed-tools: Bash") {
		t.Error("generated skill must not grant bare Bash in allowed-tools")
	}
	// Only URLs in the body are a problem; the body starts after the second "---".
	parts := strings.SplitN(got, "---", 3)
	if len(parts) >= 3 && (strings.Contains(parts[2], "http://") || strings.Contains(parts[2], "https://")) {
		t.Error("generated skill body must not reference external URLs (CSKILL-020)")
	}
	// Emitted text must not contain literal trigger patterns that cause the
	// generated skill to self-flag under trustabl scan.
	if strings.Contains(got, "!`") {
		t.Error("generated skill body must not contain !` (inline-exec pattern, CSKILL-002)")
	}
	if strings.Contains(got, "ignore previous instructions") {
		t.Error("generated skill body must not contain the injection phrase (CSKILL-040)")
	}
}

func TestMatchConditionForScope_ToolScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeTool, rules.MatchExpr{})
	want := "When defining a tool."
	if got != want {
		t.Errorf("matchConditionForScope(tool) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_AgentScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeAgent, rules.MatchExpr{})
	want := "When declaring an agent."
	if got != want {
		t.Errorf("matchConditionForScope(agent) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_RepoScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeRepo, rules.MatchExpr{})
	want := "For any repo using this SDK."
	if got != want {
		t.Errorf("matchConditionForScope(repo) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_SubagentScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeSubagent, rules.MatchExpr{})
	want := "When declaring a subagent."
	if got != want {
		t.Errorf("matchConditionForScope(subagent) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_SkillScope_DelegatesToExisting(t *testing.T) {
	boolTrue := true
	expr := rules.MatchExpr{SkillAllowsUnrestrictedShell: &boolTrue}
	got := matchConditionForScope(models.ScopeSkill, expr)
	want := "Every skill — always verify allowed-tools before emitting."
	if got != want {
		t.Errorf("matchConditionForScope(skill) = %q, want %q", got, want)
	}
}

func TestGenerateCombined_GoldenFile(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	if len(policies) == 0 {
		t.Fatal("no policies loaded from fixture")
	}

	stamp := Stamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
	}
	got := GenerateCombined(stamp.Categories, policies, stamp)

	goldenPath := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "expected", "SKILL.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file at %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file not found at %s; run:\n  go test ./internal/forge/... -update", goldenPath)
	}
	if got != string(wantBytes) {
		t.Errorf("GenerateCombined() does not match golden file %s\n\n%s",
			goldenPath, lineDiff(string(wantBytes), got))
	}
}

func TestGenerateCombined_Deterministic(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := Stamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
	}
	a := GenerateCombined(stamp.Categories, policies, stamp)
	b := GenerateCombined(stamp.Categories, policies, stamp)
	if a != b {
		t.Error("GenerateCombined is not deterministic")
	}
}

func TestGenerateCombined_UnknownCategorySkipped(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := Stamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryMCP}, // not in fixture
	}
	got := GenerateCombined(stamp.Categories, policies, stamp)
	// should produce valid frontmatter + header but no rule sections
	if !strings.Contains(got, "name: trustabl-pre-coding") {
		t.Error("expected frontmatter in output")
	}
	if strings.Contains(got, "#### [") {
		t.Error("expected no rule blocks when category has no rules in fixture")
	}
}

// lineDiff produces a simple line-oriented diff for test failure messages.
func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	var sb strings.Builder
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	diffs := 0
	for i := 0; i < max && diffs < 20; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&sb, "line %d:\n  want: %q\n  got:  %q\n", i+1, w, g)
			diffs++
		}
	}
	if diffs == 0 && want != got {
		fmt.Fprintf(&sb, "(content differs but all compared lines match — likely trailing newline or length difference)")
	}
	return sb.String()
}
