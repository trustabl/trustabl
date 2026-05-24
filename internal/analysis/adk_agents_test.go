package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverADKAgents_LlmAgentMinimal(t *testing.T) {
	src := `from google.adk.agents import LlmAgent

root = LlmAgent(
    name="root",
    model="gemini-2.5-flash",
    instruction="Be helpful.",
)
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKGoogleADK {
		t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKGoogleADK)
	}
	if a.Class != "LlmAgent" {
		t.Errorf("Class: got %q, want %q", a.Class, "LlmAgent")
	}
	if a.Name != "root" {
		t.Errorf("Name: got %q, want %q", a.Name, "root")
	}
	if a.FilePath != "main.py" {
		t.Errorf("FilePath: got %q, want %q", a.FilePath, "main.py")
	}
}
