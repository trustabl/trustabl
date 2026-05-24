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

func TestDiscoverADKAgents_AllClasses(t *testing.T) {
	src := `from google.adk.agents import LlmAgent, Agent, SequentialAgent, ParallelAgent, LoopAgent, LanggraphAgent

a = LlmAgent(name="a")
b = Agent(name="b")
c = SequentialAgent(name="c", sub_agents=[a])
d = ParallelAgent(name="d", sub_agents=[a, b])
e = LoopAgent(name="e", sub_agents=[a])
f = LanggraphAgent(name="f")
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})
	if len(agents) != 6 {
		t.Fatalf("got %d agents, want 6", len(agents))
	}
	wantByName := map[string]string{
		"a": "LlmAgent",
		"b": "LlmAgent", // alias normalization
		"c": "SequentialAgent",
		"d": "ParallelAgent",
		"e": "LoopAgent",
		"f": "LanggraphAgent",
	}
	for _, a := range agents {
		wantClass, ok := wantByName[a.Name]
		if !ok {
			t.Errorf("unexpected agent name %q", a.Name)
			continue
		}
		if a.Class != wantClass {
			t.Errorf("agent %q: Class = %q, want %q", a.Name, a.Class, wantClass)
		}
		if a.SDK != models.SDKGoogleADK {
			t.Errorf("agent %q: SDK = %q, want google_adk", a.Name, a.SDK)
		}
	}
}

func TestDiscoverADKAgents_ImportGate(t *testing.T) {
	// Agent() in a file with no google.adk import must NOT be classified as ADK.
	src := `from agents import Agent

a = Agent(name="oai_agent")
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("got %d agents from OpenAI-import file, want 0", len(agents))
	}
}

func TestDiscoverADKAgents_OpaqueOnSplat(t *testing.T) {
	src := `from google.adk.agents import LlmAgent

cfg = {"name": "a"}
a = LlmAgent(**cfg)
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if !agents[0].Opaque {
		t.Errorf("Opaque: got false, want true (LlmAgent(**cfg))")
	}
}

func TestDiscoverADKTools_FunctionToolWrapping(t *testing.T) {
	src := `from google.adk.agents import LlmAgent
from google.adk.tools import FunctionTool

def get_weather(city: str) -> str:
    """Look up the weather for a city."""
    return "sunny"

def no_docs(x):
    return x

root = LlmAgent(
    name="root",
    tools=[FunctionTool(get_weather), FunctionTool(no_docs)],
)
`
	pf := parsePyFile(t, "main.py", src)
	tools := analysis.DiscoverADKTools([]analysis.ParsedFile{pf})
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	byName := map[string]models.ToolDef{}
	for _, td := range tools {
		byName[td.Name] = td
	}
	weather, ok := byName["get_weather"]
	if !ok {
		t.Fatal("get_weather tool not found")
	}
	if weather.Kind != models.KindADKFunctionTool {
		t.Errorf("Kind: got %q, want %q", weather.Kind, models.KindADKFunctionTool)
	}
	if weather.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", weather.Language)
	}
	if weather.Description == "" {
		t.Errorf("Description: got empty, want docstring text")
	}
	if !weather.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true")
	}
	nodocs := byName["no_docs"]
	if nodocs.Description != "" {
		t.Errorf("no_docs Description: got %q, want empty", nodocs.Description)
	}
	if nodocs.HasTypedParams {
		t.Errorf("no_docs HasTypedParams: got true, want false")
	}
}
