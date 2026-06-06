package scanner_test

import (
	"strings"
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
		models.SDKOpenAIAgents, models.SDKClaudeAgentSDK, models.SDKMCP, models.SDKGoogleADK,
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
		{Class: "Agent", Language: models.LanguagePython, Location: models.Location{FilePath: "main.py", Line: 5}, Opaque: true},
		{Class: "Agent", Language: models.LanguagePython, Location: models.Location{FilePath: "main.py", Line: 20}, Opaque: false},
		{Class: "Agent", Language: models.LanguagePython, Location: models.Location{FilePath: "main.py", Line: 30}, Opaque: true},
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

func TestEmitCoverageMETA_FiresWhenSDKHasNoApplicableRule(t *testing.T) {
	// Claude SDK observed, but the registry reports no applicable category
	// for it (e.g. only tool rules loaded and zero tools discovered).
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}}
	applicable := map[models.DetectorCategory]bool{} // nothing applied
	findings := scanner.EmitCoverageMETA(applicable, inv)
	if len(findings) != 1 || findings[0].RuleID != "META-004" {
		t.Fatalf("expected one META-004, got %+v", findings)
	}
}

func TestEmitCoverageMETA_SilentWhenCategoryApplicable(t *testing.T) {
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}}
	applicable := map[models.DetectorCategory]bool{models.CategoryClaudeSDK: true}
	if f := scanner.EmitCoverageMETA(applicable, inv); len(f) != 0 {
		t.Errorf("expected no META-004 when category applicable, got %+v", f)
	}
}

func TestEmitCoverageMETA_SilentForUnmappedSDK(t *testing.T) {
	// MCP/openshell/unknown SDKs are deliberately not covered by META-004.
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP, models.SDK("langgraph")}}
	if f := scanner.EmitCoverageMETA(map[models.DetectorCategory]bool{}, inv); len(f) != 0 {
		t.Errorf("expected no META-004 for unmapped SDKs, got %+v", f)
	}
}

func TestEmitSkippedRulesMETA_SilentWhenNothingSkipped(t *testing.T) {
	if f := scanner.EmitSkippedRulesMETA(nil); len(f) != 0 {
		t.Errorf("expected no META-005 when nothing was skipped, got %+v", f)
	}
	if f := scanner.EmitSkippedRulesMETA([]string{}); len(f) != 0 {
		t.Errorf("expected no META-005 for an empty skip slice, got %+v", f)
	}
}

func TestEmitSkippedRulesMETA_EmitsInfoFinding(t *testing.T) {
	findings := scanner.EmitSkippedRulesMETA([]string{"CSKILL-001", "CSKILL-002"})
	if len(findings) != 1 {
		t.Fatalf("expected exactly one META-005 finding, got %+v", findings)
	}
	f := findings[0]
	if f.RuleID != "META-005" {
		t.Errorf("RuleID = %q, want META-005", f.RuleID)
	}
	if f.Severity != models.SeverityInfo {
		t.Errorf("Severity = %q, want info", f.Severity)
	}
	// The finding must name how many rules were skipped and which ones, so the
	// report itself (not just the JSON RulesSkipped field) is honest about the
	// degraded scan.
	if !strings.Contains(f.Explanation, "2 rule(s)") {
		t.Errorf("explanation should state the count, got: %q", f.Explanation)
	}
	if !strings.Contains(f.Explanation, "CSKILL-001") || !strings.Contains(f.Explanation, "CSKILL-002") {
		t.Errorf("explanation should list the skipped IDs, got: %q", f.Explanation)
	}
}

// TestEmitSkippedRulesMETA_Deterministic guards the byte-stable-report contract:
// the finding text must not depend on the order (or duplication) of the IDs the
// loader returned. Two differently-ordered, differently-deduped inputs naming the
// same set of rules must produce identical findings.
func TestEmitSkippedRulesMETA_Deterministic(t *testing.T) {
	a := scanner.EmitSkippedRulesMETA([]string{"B-2", "A-1", "C-3"})
	b := scanner.EmitSkippedRulesMETA([]string{"C-3", "A-1", "B-2", "A-1"})
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected one finding each, got %d and %d", len(a), len(b))
	}
	if a[0].Explanation != b[0].Explanation {
		t.Errorf("explanation is order/dup-sensitive:\n a=%q\n b=%q", a[0].Explanation, b[0].Explanation)
	}
	// Deduped: three distinct IDs, reported as "3 rule(s)".
	if !strings.Contains(b[0].Explanation, "3 rule(s)") {
		t.Errorf("duplicate IDs should be deduped to 3, got: %q", b[0].Explanation)
	}
}
