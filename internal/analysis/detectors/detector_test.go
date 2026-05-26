package detectors

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// fake detectors — minimal implementations to exercise Registry wiring.

type fakeTool struct {
	id  string
	cat models.DetectorCategory
}

func (f fakeTool) RuleID() string                    { return f.id }
func (f fakeTool) Category() models.DetectorCategory { return f.cat }
func (f fakeTool) Applies(models.ToolDef) bool       { return true }
func (f fakeTool) Detect(models.ToolDef, analysis.ParsedFile, models.RepoInventory) []models.Finding {
	return nil
}

type fakeAgent struct {
	id  string
	cat models.DetectorCategory
}

func (f fakeAgent) RuleID() string                    { return f.id }
func (f fakeAgent) Category() models.DetectorCategory { return f.cat }
func (f fakeAgent) Applies(models.AgentDef) bool      { return true }
func (f fakeAgent) Detect(models.AgentDef, models.RepoInventory) []models.Finding {
	return nil
}

type fakeRepo struct {
	id  string
	cat models.DetectorCategory
}

func (f fakeRepo) RuleID() string                                        { return f.id }
func (f fakeRepo) Category() models.DetectorCategory                     { return f.cat }
func (f fakeRepo) Applies(models.RepoProfile, models.RepoInventory) bool { return true }
func (f fakeRepo) Detect(models.RepoProfile, models.RepoInventory) []models.Finding {
	return nil
}

type fakeSubagentDetector struct {
	id    string
	cat   models.DetectorCategory
	fires bool
}

func (d fakeSubagentDetector) RuleID() string                    { return d.id }
func (d fakeSubagentDetector) Category() models.DetectorCategory { return d.cat }
func (d fakeSubagentDetector) Applies(models.SubagentDef) bool   { return true }
func (d fakeSubagentDetector) Detect(s models.SubagentDef, _ models.RepoInventory) []models.Finding {
	if !d.fires {
		return nil
	}
	return []models.Finding{{RuleID: d.id, ToolName: s.Name}}
}

func newTestRegistry() *Registry {
	return New(
		[]ToolDetector{
			fakeTool{id: "CSDK-001", cat: models.CategoryClaudeSDK},
			fakeTool{id: "OAI-001", cat: models.CategoryOpenAISDK},
		},
		[]AgentDetector{
			fakeAgent{id: "OAI-101", cat: models.CategoryOpenAISDK},
		},
		[]RepoDetector{
			fakeRepo{id: "OAI-201", cat: models.CategoryOpenAISDK},
		},
		nil,
	)
}

func TestRegistryCount(t *testing.T) {
	if got := newTestRegistry().Count(); got != 4 {
		t.Fatalf("Count() = %d, want 4", got)
	}
	if got := New(nil, nil, nil, nil).Count(); got != 0 {
		t.Fatalf("empty Count() = %d, want 0", got)
	}
}

func TestRegistrySubset(t *testing.T) {
	r := newTestRegistry()

	// Only claude_sdk: the single claude tool detector survives.
	claude := r.Subset(models.CategoryClaudeSDK)
	if got := claude.Count(); got != 1 {
		t.Fatalf("Subset(claude_sdk).Count() = %d, want 1", got)
	}

	// Only openai_sdk: one tool + one agent + one repo detector survive.
	openai := r.Subset(models.CategoryOpenAISDK)
	if got := openai.Count(); got != 3 {
		t.Fatalf("Subset(openai_sdk).Count() = %d, want 3", got)
	}

	// Both categories: everything survives.
	both := r.Subset(models.CategoryClaudeSDK, models.CategoryOpenAISDK)
	if got := both.Count(); got != 4 {
		t.Fatalf("Subset(claude_sdk, openai_sdk).Count() = %d, want 4", got)
	}

	// A category with no detectors yields an empty registry.
	none := r.Subset(models.CategoryOpenShell)
	if got := none.Count(); got != 0 {
		t.Fatalf("Subset(openshell).Count() = %d, want 0", got)
	}

	// Subset must not mutate the original registry.
	if got := r.Count(); got != 4 {
		t.Fatalf("original registry mutated by Subset: Count() = %d, want 4", got)
	}
}

func TestRegistry_RunsSubagentDetectors(t *testing.T) {
	reg := New(nil, nil, nil, []SubagentDetector{
		fakeSubagentDetector{id: "SUB-1", cat: "claude_sdk", fires: true},
	})
	inv := models.RepoInventory{
		Subagents: []models.SubagentDef{{Name: "searcher", Location: models.Location{FilePath: ".claude/agents/searcher.md"}}},
	}
	findings := reg.Run(models.RepoProfile{}, inv, nil, nil)
	if len(findings) != 1 || findings[0].RuleID != "SUB-1" {
		t.Fatalf("expected one SUB-1 finding, got %+v", findings)
	}
	if reg.Count() != 1 {
		t.Errorf("Count: got %d, want 1", reg.Count())
	}
	cats := reg.ApplicableCategories(models.RepoProfile{}, inv)
	if !cats["claude_sdk"] {
		t.Errorf("ApplicableCategories missing claude_sdk: %+v", cats)
	}
}
