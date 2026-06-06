package detectors

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestRunInvokesOnEntityPerToolAndAgent(t *testing.T) {
	inv := models.RepoInventory{
		Tools:  []models.ToolDef{{Name: "a"}, {Name: "b"}},
		Agents: []models.AgentDef{{Name: "agent1", Language: models.LanguagePython}},
	}
	r := New(nil, nil, nil, nil, nil)
	var labels []string
	r.Run(models.RepoProfile{}, inv, []analysis.ParsedFile{}, func(label string) {
		labels = append(labels, label)
	})
	if len(labels) != 3 {
		t.Fatalf("onEntity called %d times, want 3 (2 tools + 1 agent): %v", len(labels), labels)
	}
}
