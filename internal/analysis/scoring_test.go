package analysis_test

import (
	"math"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

const eps = 1e-9

func tool(name, file string) models.ToolDef {
	return models.ToolDef{Name: name, Kind: models.KindClaudeSDKTool, Location: models.Location{FilePath: file}}
}

// arithMean is the plain average of surface scores — the baseline the
// badness-weighted mean must sit at or below.
func arithMean(surfaces []models.SurfaceReadiness) float64 {
	if len(surfaces) == 0 {
		return 1.0
	}
	var sum float64
	for _, s := range surfaces {
		sum += s.Score
	}
	return sum / float64(len(surfaces))
}

// TestScore_ExcludesMetaAndUnmatchedFindings: a META finding (empty scope) and a
// tool-scoped finding for a tool that doesn't exist must not create surfaces and
// must not move the score. This is the behavior that produced "100% with
// findings": those findings are simply not posture defects on any surface.
func TestScore_ExcludesMetaAndUnmatchedFindings(t *testing.T) {
	tools := []models.ToolDef{tool("search", "a.py")}
	findings := []models.Finding{
		{RuleID: "META-004", Severity: models.SeverityInfo, Confidence: 1.0}, // Scope == ""
		{RuleID: "GHOST", Scope: models.ScopeTool, ToolName: "nope", FilePath: "z.py", Severity: models.SeverityHigh, Confidence: 1.0},
	}
	surfaces, overall := analysis.Score(tools, nil, nil, findings)
	if len(surfaces) != 1 {
		t.Fatalf("got %d surfaces, want 1 (just the tool): %+v", len(surfaces), surfaces)
	}
	if surfaces[0].FindingCount != 0 {
		t.Errorf("tool FindingCount: got %d, want 0", surfaces[0].FindingCount)
	}
	if math.Abs(overall-1.0) > eps {
		t.Errorf("overall: got %v, want 1.0", overall)
	}
}

// TestScore_AgentFindingLowersOverall is the core regression: a repo with no
// tools but a high-severity agent finding must NOT score 100%.
func TestScore_AgentFindingLowersOverall(t *testing.T) {
	agents := []models.AgentDef{
		{Class: "AgentDefinition", Name: "Planner", Location: models.Location{FilePath: "p.py"}},
	}
	findings := []models.Finding{
		{RuleID: "CSDK-201", Scope: models.ScopeAgent, ToolName: "Planner", FilePath: "p.py", Severity: models.SeverityCritical, Confidence: 1.0},
	}
	surfaces, overall := analysis.Score(nil, agents, nil, findings)
	if len(surfaces) != 1 || surfaces[0].Kind != models.ScopeAgent || surfaces[0].Name != "Planner" {
		t.Fatalf("want one agent surface 'Planner', got %+v", surfaces)
	}
	if surfaces[0].FindingCount != 1 {
		t.Errorf("agent FindingCount: got %d, want 1", surfaces[0].FindingCount)
	}
	// critical weight 1.0, conf 1.0 -> score = 1 - 1.0/3 = 0.6667; single surface
	// -> overall equals that score.
	if overall >= 1.0 {
		t.Fatalf("overall: got %v, must be < 1.0 (agent has a critical finding)", overall)
	}
	if math.Abs(overall-(1.0-1.0/3.0)) > 1e-6 {
		t.Errorf("overall: got %v, want ~0.6667", overall)
	}
}

// TestScore_SubagentFindingLowersOverall: subagent findings create a subagent
// surface and lower the overall.
func TestScore_SubagentFindingLowersOverall(t *testing.T) {
	subs := []models.SubagentDef{
		{Name: "deployer", Location: models.Location{FilePath: ".claude/agents/deployer.md"}},
	}
	findings := []models.Finding{
		{RuleID: "CSDK-110", Scope: models.ScopeSubagent, ToolName: "deployer", FilePath: ".claude/agents/deployer.md", Severity: models.SeverityHigh, Confidence: 1.0},
	}
	surfaces, overall := analysis.Score(nil, nil, subs, findings)
	if len(surfaces) != 1 || surfaces[0].Kind != models.ScopeSubagent {
		t.Fatalf("want one subagent surface, got %+v", surfaces)
	}
	if overall >= 1.0 {
		t.Errorf("overall: got %v, must be < 1.0", overall)
	}
}

// TestScore_RepoFindingsPoolIntoOneSurface: every repo-scoped finding lands in a
// single repo surface, created only because repo findings exist.
func TestScore_RepoFindingsPoolIntoOneSurface(t *testing.T) {
	findings := []models.Finding{
		{RuleID: "OAI-201", Scope: models.ScopeRepo, Severity: models.SeverityMedium, Confidence: 1.0},
		{RuleID: "REPO-002", Scope: models.ScopeRepo, Severity: models.SeverityLow, Confidence: 1.0},
	}
	surfaces, overall := analysis.Score(nil, nil, nil, findings)
	if len(surfaces) != 1 || surfaces[0].Kind != models.ScopeRepo {
		t.Fatalf("want one repo surface, got %+v", surfaces)
	}
	if surfaces[0].Name != "" {
		t.Errorf("repo surface Name: got %q, want \"\"", surfaces[0].Name)
	}
	if surfaces[0].FindingCount != 2 {
		t.Errorf("repo FindingCount: got %d, want 2 (pooled)", surfaces[0].FindingCount)
	}
	if overall >= 1.0 {
		t.Errorf("overall: got %v, must be < 1.0", overall)
	}
}

// TestScore_NoRepoSurfaceWithoutRepoFindings: a clean repo gets no repo row.
func TestScore_NoRepoSurfaceWithoutRepoFindings(t *testing.T) {
	tools := []models.ToolDef{tool("search", "a.py")}
	surfaces, overall := analysis.Score(tools, nil, nil, nil)
	for _, s := range surfaces {
		if s.Kind == models.ScopeRepo {
			t.Errorf("unexpected repo surface on a repo with no repo findings: %+v", surfaces)
		}
	}
	if math.Abs(overall-1.0) > eps {
		t.Errorf("overall: got %v, want 1.0", overall)
	}
}

// TestScore_DistinguishesSameNamedToolsAcrossFiles: keep same-named tools in
// different files distinct (keyed by file).
func TestScore_DistinguishesSameNamedToolsAcrossFiles(t *testing.T) {
	tools := []models.ToolDef{tool("search", "a.py"), tool("search", "b.py")}
	findings := []models.Finding{
		{RuleID: "CSDK-003", Scope: models.ScopeTool, ToolName: "search", FilePath: "a.py", Severity: models.SeverityHigh, Confidence: 1.0},
	}
	surfaces, _ := analysis.Score(tools, nil, nil, findings)
	if len(surfaces) != 2 {
		t.Fatalf("got %d surfaces, want 2 (one per file): %+v", len(surfaces), surfaces)
	}
	var withFinding, clean *models.SurfaceReadiness
	for i := range surfaces {
		switch surfaces[i].FilePath {
		case "a.py":
			withFinding = &surfaces[i]
		case "b.py":
			clean = &surfaces[i]
		}
	}
	if withFinding == nil || withFinding.FindingCount != 1 {
		t.Errorf("a.py search: want FindingCount=1, got %+v", withFinding)
	}
	if clean == nil || clean.FindingCount != 0 || clean.Score != 1.0 {
		t.Errorf("b.py search: want clean, got %+v", clean)
	}
}

// TestScore_CleanSurfacesDiluteBadOnes: the accepted tradeoff. The same bad
// agent reads higher when surrounded by clean tools than when alone.
func TestScore_CleanSurfacesDiluteBadOnes(t *testing.T) {
	agents := []models.AgentDef{
		{Class: "AgentDefinition", Name: "Planner", Location: models.Location{FilePath: "p.py"}},
	}
	bad := models.Finding{RuleID: "CSDK-201", Scope: models.ScopeAgent, ToolName: "Planner", FilePath: "p.py", Severity: models.SeverityCritical, Confidence: 1.0}

	_, alone := analysis.Score(nil, agents, nil, []models.Finding{bad})

	manyTools := make([]models.ToolDef, 10)
	for i := range manyTools {
		manyTools[i] = tool("t"+string(rune('0'+i)), "t.py")
	}
	_, surrounded := analysis.Score(manyTools, agents, nil, []models.Finding{bad})

	if !(surrounded > alone) {
		t.Errorf("dilution: surrounded (%v) must read higher than alone (%v)", surrounded, alone)
	}
}

// TestScore_OverallPulledTowardWeakSurface: the badness-weighted mean must sit
// strictly below the plain arithmetic mean (pulled toward the weak surface) yet
// above the weakest surface (no min-cliff), whenever surface scores differ.
func TestScore_OverallPulledTowardWeakSurface(t *testing.T) {
	tools := []models.ToolDef{tool("clean", "a.py"), tool("bad", "b.py")}
	findings := []models.Finding{
		{RuleID: "CSDK-003", Scope: models.ScopeTool, ToolName: "bad", FilePath: "b.py", Severity: models.SeverityCritical, Confidence: 1.0},
	}
	surfaces, overall := analysis.Score(tools, nil, nil, findings)
	mean := arithMean(surfaces)
	var worst float64 = 1.0
	for _, s := range surfaces {
		if s.Score < worst {
			worst = s.Score
		}
	}
	if !(overall < mean) {
		t.Errorf("overall (%v) must be < arithmetic mean (%v)", overall, mean)
	}
	if !(overall > worst) {
		t.Errorf("overall (%v) must be > weakest surface (%v) — no min-cliff", overall, worst)
	}
	// Pin the exact blended value so a change to the aggregation formula
	// (blendK/saturation) is caught, not just the relational invariants.
	// clean=1.0 (w=1), bad=2/3 (w=2.0) -> overall = (1 + 2*2/3)/3 = 7/9.
	if math.Abs(overall-7.0/9.0) > 1e-6 {
		t.Errorf("overall: got %v, want 7/9 ≈ 0.77778", overall)
	}
}

// TestScore_EmptyReturnsOne: a genuinely empty repo (no surfaces) scores 1.0.
func TestScore_EmptyReturnsOne(t *testing.T) {
	surfaces, overall := analysis.Score(nil, nil, nil, nil)
	if len(surfaces) != 0 {
		t.Fatalf("want 0 surfaces, got %+v", surfaces)
	}
	if math.Abs(overall-1.0) > eps {
		t.Errorf("overall: got %v, want 1.0", overall)
	}
}

// TestProject_EmptyReturnsAllOne: a clean repo (tools, no findings) projects to
// 1.0 at every tier.
func TestProject_EmptyReturnsAllOne(t *testing.T) {
	tools := []models.ToolDef{tool("search", "a.py")}
	p := analysis.Project(tools, nil, nil, nil)
	for name, v := range map[string]float64{
		"fix_critical": p.FixCritical, "fix_high": p.FixHigh, "fix_medium": p.FixMedium,
		"fix_low": p.FixLow, "fix_all": p.FixAll,
	} {
		if math.Abs(v-1.0) > eps {
			t.Errorf("%s: got %v, want 1.0", name, v)
		}
	}
}

// TestProject_MonotonicAndAboveOverall: with findings spread across surfaces and
// severities, projections are non-decreasing crit→all, each ≥ the unprojected
// overall, and fix_all == 1.0 (everything resolved → all surfaces clean).
func TestProject_MonotonicAndAboveOverall(t *testing.T) {
	tools := []models.ToolDef{tool("a", "a.py"), tool("b", "b.py"), tool("c", "c.py")}
	findings := []models.Finding{
		{RuleID: "X1", Scope: models.ScopeTool, ToolName: "a", FilePath: "a.py", Severity: models.SeverityCritical, Confidence: 1.0},
		{RuleID: "X2", Scope: models.ScopeTool, ToolName: "b", FilePath: "b.py", Severity: models.SeverityHigh, Confidence: 1.0},
		{RuleID: "X3", Scope: models.ScopeTool, ToolName: "c", FilePath: "c.py", Severity: models.SeverityLow, Confidence: 1.0},
	}
	_, overall := analysis.Score(tools, nil, nil, findings)
	p := analysis.Project(tools, nil, nil, findings)

	if p.FixCritical < overall-eps {
		t.Errorf("fix_critical (%v) must be >= overall (%v)", p.FixCritical, overall)
	}
	if !(p.FixCritical > overall) {
		t.Errorf("resolving the critical finding must raise the score: fix_critical=%v overall=%v", p.FixCritical, overall)
	}
	chain := []float64{p.FixCritical, p.FixHigh, p.FixMedium, p.FixLow, p.FixAll}
	for i := 1; i < len(chain); i++ {
		if chain[i] < chain[i-1]-eps {
			t.Errorf("projection not monotonic at tier %d: %v then %v (full: %+v)", i, chain[i-1], chain[i], p)
		}
	}
	if math.Abs(p.FixAll-1.0) > eps {
		t.Errorf("fix_all: got %v, want 1.0 (all findings resolved)", p.FixAll)
	}
}

// TestProject_InfoResolvedOnlyAtFixAll: an info-only finding survives every tier
// except fix_all, so the lower tiers equal the unprojected overall and only
// fix_all climbs to 1.0. Guards the tier boundary (info has rank 0).
func TestProject_InfoResolvedOnlyAtFixAll(t *testing.T) {
	tools := []models.ToolDef{tool("a", "a.py")}
	findings := []models.Finding{
		{RuleID: "I1", Scope: models.ScopeTool, ToolName: "a", FilePath: "a.py", Severity: models.SeverityInfo, Confidence: 1.0},
	}
	_, overall := analysis.Score(tools, nil, nil, findings)
	p := analysis.Project(tools, nil, nil, findings)
	for name, v := range map[string]float64{
		"fix_critical": p.FixCritical, "fix_high": p.FixHigh, "fix_medium": p.FixMedium, "fix_low": p.FixLow,
	} {
		if math.Abs(v-overall) > eps {
			t.Errorf("%s: got %v, want overall %v (info finding must not resolve below fix_all)", name, v, overall)
		}
	}
	if math.Abs(p.FixAll-1.0) > eps {
		t.Errorf("fix_all: got %v, want 1.0", p.FixAll)
	}
	if overall >= 1.0 {
		t.Errorf("sanity: overall with an info finding (%v) should be < 1.0", overall)
	}
}
