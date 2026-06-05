package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A CrewAI Agent(...) in a file that imports `crewai` is emitted as one AgentDef
// with SDK=SDKCrewAI, Class="Agent", and its constructor kwargs captured so the
// agent-scope rules (CREW-101/102/104) can read allow_code_execution etc.
func TestCrewAIAgent_ConstructorCaptured(t *testing.T) {
	src := `from crewai import Agent

researcher = Agent(
    role="Researcher",
    goal="Find facts",
    backstory="An expert.",
    allow_code_execution=True,
)
`
	pf := parsePyFile(t, "crew.py", src)
	agents := analysis.DiscoverCrewAIAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKCrewAI {
		t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKCrewAI)
	}
	if a.Class != "Agent" {
		t.Errorf("Class: got %q, want Agent", a.Class)
	}
	if a.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", a.Language)
	}
	if a.VarName != "researcher" {
		t.Errorf("VarName: got %q, want researcher", a.VarName)
	}
	// role= is the human-facing label; discovery falls back to it for Name.
	if a.Name != "Researcher" {
		t.Errorf("Name: got %q, want Researcher", a.Name)
	}
	kw := a.Kwargs.Children["allow_code_execution"]
	if kw == nil || kw.Value == nil {
		t.Fatalf("allow_code_execution kwarg not captured: %+v", a.Kwargs)
	}
	if kw.Value.Text != "True" {
		t.Errorf("allow_code_execution value: got %q, want True", kw.Value.Text)
	}
}

// The `@tool` decorator imported from `crewai.tools` is discovered as a
// KindCrewAITool ToolDef via the shared decorator path (import-gated).
func TestCrewAITool_DecoratorDiscovered(t *testing.T) {
	src := `from crewai.tools import tool

@tool("search")
def search(q: str) -> str:
    """Search the web."""
    return q
`
	pf := parsePyFile(t, "tools.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Kind != models.KindCrewAITool {
		t.Errorf("Kind: got %q, want %q", tool.Kind, models.KindCrewAITool)
	}
	if tool.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", tool.Language)
	}
	if tool.Name != "search" {
		t.Errorf("Name: got %q, want search", tool.Name)
	}
}

// Collision guard: the class name `Agent` and the bare `@tool` decorator are
// shared with other SDKs, so a file that imports NEITHER crewai nor another
// agent SDK must not yield any CrewAI agent or tool. DiscoverCrewAIAgents is
// import-gated, so it emits nothing; the bare @tool falls through to the
// historical Claude-SDK default (not crewai).
func TestCrewAIAgent_GateExcludesUnimported(t *testing.T) {
	src := `def Agent(**kw):
    return kw

def tool(fn):
    return fn

planner = Agent(role="x", allow_code_execution=True)

@tool
def helper(q: str) -> str:
    """Help."""
    return q
`
	pf := parsePyFile(t, "plain.py", src)
	if agents := analysis.DiscoverCrewAIAgents([]analysis.ParsedFile{pf}); len(agents) != 0 {
		t.Errorf("unimported Agent must not be a CrewAI agent; got %+v", agents)
	}
	for _, tl := range analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}) {
		if tl.Kind == models.KindCrewAITool {
			t.Errorf("unimported @tool must not be a CrewAI tool; got %+v", tl)
		}
	}
}

// Collision guard, the other direction: an `Agent(...)` in a file importing the
// OpenAI Agents SDK (`from agents import Agent`) stays an OpenAI agent and is
// never re-attributed to CrewAI. DiscoverCrewAIAgents (import-gated to crewai)
// emits nothing for it, and DiscoverAgents keeps it as SDKOpenAIAgents.
func TestCrewAIAgent_DoesNotClaimOpenAIAgent(t *testing.T) {
	src := `from agents import Agent

agent = Agent(name="ops", instructions="Run ops.", model="gpt-4")
`
	pf := parsePyFile(t, "oai.py", src)
	if crew := analysis.DiscoverCrewAIAgents([]analysis.ParsedFile{pf}); len(crew) != 0 {
		t.Errorf("OpenAI Agent must not be discovered as CrewAI; got %+v", crew)
	}
	oai := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(oai) != 1 {
		t.Fatalf("got %d OpenAI agents, want 1", len(oai))
	}
	if oai[0].SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK: got %q, want %q", oai[0].SDK, models.SDKOpenAIAgents)
	}
}
