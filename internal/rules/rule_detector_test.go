package rules

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestAgentRuleDetector_LanguageGate_RejectsTSAgentForPythonRule(t *testing.T) {
	d := agentRuleDetector{rule: RuleDef{
		ID:        "TEST-101",
		Language:  models.LanguagePython,
		AppliesTo: []string{"claude_agent_definition"},
	}}
	tsAgent := models.AgentDef{
		SDK:      models.SDKClaudeAgentSDK,
		Class:    "AgentDefinition",
		Language: models.LanguageTypeScript,
	}
	if d.Applies(tsAgent) {
		t.Fatal("expected Applies()=false for TS agent vs Python rule, got true")
	}
}

func TestAgentRuleDetector_LanguageGate_AcceptsMatchingLanguage(t *testing.T) {
	d := agentRuleDetector{rule: RuleDef{
		ID:        "TEST-101",
		Language:  models.LanguagePython,
		AppliesTo: []string{"claude_agent_definition"},
	}}
	pyAgent := models.AgentDef{
		SDK:      models.SDKClaudeAgentSDK,
		Class:    "AgentDefinition",
		Language: models.LanguagePython,
	}
	if !d.Applies(pyAgent) {
		t.Fatal("expected Applies()=true for matching language, got false")
	}
}

func TestSubagentRuleDetector_AppliesAndDetects(t *testing.T) {
	d := NewSubagentRuleDetector(RuleDef{
		ID:        "TEST-SUB",
		Scope:     models.ScopeSubagent,
		AppliesTo: []string{"claude_subagent"},
		Severity:  models.SeverityHigh,
		Match:     MatchExpr{SubagentGrantsTool: []string{"Bash"}},
	})
	grants := models.SubagentDef{Name: "searcher", Location: models.Location{FilePath: ".claude/agents/searcher.md"},
		Tools: []string{"Read", "Bash"}}
	if !d.Applies(grants) {
		t.Fatal("expected Applies()=true for claude_subagent")
	}
	findings := d.Detect(grants, models.RepoInventory{})
	if len(findings) != 1 || findings[0].RuleID != "TEST-SUB" {
		t.Fatalf("expected one TEST-SUB finding, got %+v", findings)
	}
	if findings[0].FilePath != ".claude/agents/searcher.md" || findings[0].ToolName != "searcher" {
		t.Errorf("finding attribution wrong: %+v", findings[0])
	}
	if findings[0].Scope != models.ScopeSubagent {
		t.Errorf("subagent finding scope: got %q, want %q", findings[0].Scope, models.ScopeSubagent)
	}

	other := NewSubagentRuleDetector(RuleDef{
		ID: "X", Scope: models.ScopeSubagent, AppliesTo: []string{"claude_agent_definition"},
	})
	if other.Applies(grants) {
		t.Errorf("expected Applies()=false for non-claude_subagent appliesTo")
	}
}

// TestRepoRuleDetector_ClaudeSDKAlias guards the namespace bridge between the
// repo-scope applies_to token `claude_sdk` (a category label) and the SDK enum
// value stored in SDKsDetected, which is `claude_agent_sdk`. Without the alias
// in repoRuleDetector.Applies, a Claude repo rule loads into the registry (the
// pack gate in LoadFor maps the SDK to the claude_sdk category) but then never
// fires, because the per-rule Applies compared the raw token against the enum
// string and they differ. Every other SDK's token equals its enum string, so
// only Claude was broken.
func TestRepoRuleDetector_ClaudeSDKAlias(t *testing.T) {
	d := repoRuleDetector{rule: RuleDef{
		ID:        "TEST-REPO-CLAUDE",
		Scope:     models.ScopeRepo,
		AppliesTo: []string{"claude_sdk"},
	}}
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}}
	if !d.Applies(models.RepoProfile{}, inv) {
		t.Fatal("expected Applies()=true: claude_sdk token must match SDKClaudeAgentSDK in SDKsDetected")
	}

	// A repo without the Claude SDK must not match.
	other := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}}
	if d.Applies(models.RepoProfile{}, other) {
		t.Error("expected Applies()=false: claude_sdk rule must not fire on an OpenAI-only repo")
	}
}

// TestRepoRuleDetector_NonClaudeTokensUnaffected confirms the alias is surgical:
// openai_agents/google_adk/mcp tokens already equal their SDK enum strings and
// must keep matching by identity.
func TestRepoRuleDetector_NonClaudeTokensUnaffected(t *testing.T) {
	cases := []struct {
		token string
		sdk   models.SDK
	}{
		{"openai_agents", models.SDKOpenAIAgents},
		{"google_adk", models.SDKGoogleADK},
		{"mcp", models.SDKMCP},
	}
	for _, c := range cases {
		d := repoRuleDetector{rule: RuleDef{Scope: models.ScopeRepo, AppliesTo: []string{c.token}}}
		inv := models.RepoInventory{SDKsDetected: []models.SDK{c.sdk}}
		if !d.Applies(models.RepoProfile{}, inv) {
			t.Errorf("token %q must match SDK %q", c.token, c.sdk)
		}
	}
}

// TestFindingFromRule_RecordsScope verifies the rule's scope is stamped onto the
// emitted finding, so the scorer can route it to the right surface and exclude
// non-scored (META) findings.
func TestFindingFromRule_RecordsScope(t *testing.T) {
	cases := []models.Scope{
		models.ScopeTool, models.ScopeAgent, models.ScopeRepo, models.ScopeSubagent,
	}
	for _, sc := range cases {
		f := findingFromRule(RuleDef{ID: "X"}, sc, "f.py", 3, "thing")
		if f.Scope != sc {
			t.Errorf("scope %q: got Finding.Scope=%q", sc, f.Scope)
		}
	}
}

// TestSubagentRuleDetector_PropagatesSubagentLine guards against the regression
// where subagent findings emitted line=0 even though SubagentDef carries a real
// Line (1 = opening `---`, EndLine = closing `---`). Tools and agents already
// propagated their lines; subagents were stuck on a hardcoded 0 from when
// SubagentDef genuinely had no Location embed.
func TestSubagentRuleDetector_PropagatesSubagentLine(t *testing.T) {
	d := NewSubagentRuleDetector(RuleDef{
		ID:        "TEST-LINE",
		Scope:     models.ScopeSubagent,
		AppliesTo: []string{"claude_subagent"},
		Severity:  models.SeverityHigh,
		Match:     MatchExpr{SubagentGrantsTool: []string{"Bash"}},
	})
	sub := models.SubagentDef{
		Name:     "shelly",
		Tools:    []string{"Bash"},
		Location: models.Location{FilePath: "plugins/p/agents/shelly.md", Line: 1, EndLine: 7},
	}
	findings := d.Detect(sub, models.RepoInventory{})
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(findings))
	}
	if findings[0].Line != 1 {
		t.Errorf("finding Line = %d, want 1 (propagated from SubagentDef.Line)", findings[0].Line)
	}
}
