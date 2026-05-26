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

	other := NewSubagentRuleDetector(RuleDef{
		ID: "X", Scope: models.ScopeSubagent, AppliesTo: []string{"claude_agent_definition"},
	})
	if other.Applies(grants) {
		t.Errorf("expected Applies()=false for non-claude_subagent appliesTo")
	}
}
