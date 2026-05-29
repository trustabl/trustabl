package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// TestScore_SkipsFindingsWithoutMatchingTool guards against the historical
// "shouldn't happen, but be safe" branch that created a blank-name
// ToolReadiness bucket whenever a finding's ToolName didn't match any
// discovered tool. META findings (which have ToolName="") and agent-scoped
// findings (whose ToolName is an agent name, not a tool name) were getting
// aggregated into that fake bucket — surfacing as a confusing blank row in
// the human-readable per-tool readiness table and dragging down the overall
// score for non-tool reasons.
func TestScore_SkipsFindingsWithoutMatchingTool(t *testing.T) {
	tools := []models.ToolDef{
		{Name: "search", Kind: models.KindClaudeSDKTool},
	}
	findings := []models.Finding{
		// META finding — no ToolName, repo-scoped.
		{RuleID: "META-004", Severity: models.SeverityInfo, Confidence: 1.0},
		// Agent-scoped finding — ToolName is an agent name, not a tool name.
		{RuleID: "TEST-101", ToolName: "AIClient.queryStream", Severity: models.SeverityHigh, Confidence: 1.0},
	}
	readiness, overall := analysis.Score(tools, findings)

	// Should produce exactly ONE entry — the discovered tool. No blank-name
	// row from META, no row for the agent name.
	if len(readiness) != 1 {
		t.Fatalf("got %d readiness entries, want 1: %+v", len(readiness), readiness)
	}
	if readiness[0].ToolName != "search" {
		t.Errorf("ToolName: got %q, want \"search\"", readiness[0].ToolName)
	}
	if readiness[0].FindingCount != 0 {
		t.Errorf("FindingCount: got %d, want 0 (no tool-scoped findings)", readiness[0].FindingCount)
	}
	if overall != 1.0 {
		t.Errorf("overall: got %v, want 1.0 (no tool-scoped issues)", overall)
	}
}

// TestScore_DistinguishesSameNamedToolsAcrossFiles guards against collapsing
// two tools that share a name but live in different files into one readiness
// row. Real repos reuse tool names across modules; keying readiness by name
// alone would overwrite the first tool and pile both files' findings onto one
// row, mis-scoring both.
func TestScore_DistinguishesSameNamedToolsAcrossFiles(t *testing.T) {
	tools := []models.ToolDef{
		{Name: "search", Kind: models.KindClaudeSDKTool, Location: models.Location{FilePath: "a.py"}},
		{Name: "search", Kind: models.KindClaudeSDKTool, Location: models.Location{FilePath: "b.py"}},
	}
	findings := []models.Finding{
		// Only the a.py "search" has the finding.
		{RuleID: "CSDK-003", ToolName: "search", FilePath: "a.py", Severity: models.SeverityHigh, Confidence: 1.0},
	}
	readiness, overall := analysis.Score(tools, findings)
	if len(readiness) != 2 {
		t.Fatalf("got %d readiness entries, want 2 (one per file): %+v", len(readiness), readiness)
	}
	var withFinding, clean *models.ToolReadiness
	for i := range readiness {
		switch readiness[i].FilePath {
		case "a.py":
			withFinding = &readiness[i]
		case "b.py":
			clean = &readiness[i]
		}
	}
	if withFinding == nil || withFinding.FindingCount != 1 {
		t.Errorf("a.py search: want FindingCount=1, got %+v", withFinding)
	}
	if clean == nil || clean.FindingCount != 0 || clean.Score != 1.0 {
		t.Errorf("b.py search: want clean (0 findings, score=1.0), got %+v", clean)
	}
	if withFinding != nil && overall != withFinding.Score {
		t.Errorf("overall: got %v, want a.py search.Score=%v (weakest link)", overall, withFinding.Score)
	}
}

func TestScore_AttributesToolScopedFindingsToCorrectTool(t *testing.T) {
	tools := []models.ToolDef{
		{Name: "search", Kind: models.KindClaudeSDKTool},
		{Name: "read", Kind: models.KindClaudeSDKTool},
	}
	findings := []models.Finding{
		{RuleID: "CSDK-001", ToolName: "search", Severity: models.SeverityLow, Confidence: 1.0},
		{RuleID: "CSDK-003", ToolName: "search", Severity: models.SeverityHigh, Confidence: 1.0},
		// "read" has no findings.
	}
	readiness, overall := analysis.Score(tools, findings)
	if len(readiness) != 2 {
		t.Fatalf("got %d readiness entries, want 2: %+v", len(readiness), readiness)
	}
	var search, read *models.ToolReadiness
	for i := range readiness {
		switch readiness[i].ToolName {
		case "search":
			search = &readiness[i]
		case "read":
			read = &readiness[i]
		}
	}
	if search == nil || search.FindingCount != 2 {
		t.Errorf("search: want FindingCount=2, got %+v", search)
	}
	if read == nil || read.FindingCount != 0 || read.Score != 1.0 {
		t.Errorf("read: want clean (0 findings, score=1.0), got %+v", read)
	}
	// Overall = min(search.Score, read.Score). read=1.0, search<1.0 → overall = search.Score.
	if overall != search.Score {
		t.Errorf("overall: got %v, want search.Score=%v (weakest link)", overall, search.Score)
	}
}
