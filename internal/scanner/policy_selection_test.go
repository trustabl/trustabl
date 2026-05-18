package scanner_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

func TestSelectPolicies_EmitsMETA001ForUnauditedSDK(t *testing.T) {
	profile := models.RepoProfile{}
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDK("langgraph")}}
	findings := scanner.SelectAndEmitMETA(profile, inv)
	if len(findings) != 1 || findings[0].RuleID != "META-001" {
		t.Fatalf("expected one META-001 finding, got %+v", findings)
	}
}

func TestSelectPolicies_SilentForKnownSDKs(t *testing.T) {
	profile := models.RepoProfile{}
	inv := models.RepoInventory{SDKsDetected: []models.SDK{
		models.SDKOpenAIAgents, models.SDKClaudeAgentSDK, models.SDKMCP, models.SDKOpenShell,
	}}
	findings := scanner.SelectAndEmitMETA(profile, inv)
	for _, f := range findings {
		if f.RuleID == "META-001" {
			t.Errorf("unexpected META-001 for known SDK: %+v", f)
		}
	}
}

func TestSelectPolicies_EmitsMETA002ForDepDrift(t *testing.T) {
	profile := models.RepoProfile{SDKDeps: []models.SDKDep{{Name: "openai-agents", Source: "pyproject.toml"}}}
	inv := models.RepoInventory{SDKsDetected: nil}
	findings := scanner.SelectAndEmitMETA(profile, inv)
	var meta002 int
	for _, f := range findings {
		if f.RuleID == "META-002" {
			meta002++
		}
	}
	if meta002 != 1 {
		t.Errorf("expected 1 META-002, got %d", meta002)
	}
}

func TestSelectPolicies_SilentWhenDepAndCodeBothPresent(t *testing.T) {
	profile := models.RepoProfile{SDKDeps: []models.SDKDep{{Name: "openai-agents"}}}
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}}
	findings := scanner.SelectAndEmitMETA(profile, inv)
	for _, f := range findings {
		if f.RuleID == "META-002" {
			t.Errorf("expected no META-002, got %+v", f)
		}
	}
}

func TestSelectPolicies_EmitsMETA003PerOpaqueAgent(t *testing.T) {
	inv := models.RepoInventory{Agents: []models.AgentDef{
		{Class: "Agent", FilePath: "main.py", Line: 5, Opaque: true},
		{Class: "Agent", FilePath: "main.py", Line: 20, Opaque: false},
		{Class: "Agent", FilePath: "main.py", Line: 30, Opaque: true},
	}}
	findings := scanner.SelectAndEmitMETA(models.RepoProfile{}, inv)
	var meta003 int
	for _, f := range findings {
		if f.RuleID == "META-003" {
			meta003++
		}
	}
	if meta003 != 2 {
		t.Errorf("expected 2 META-003 (one per opaque), got %d", meta003)
	}
}
