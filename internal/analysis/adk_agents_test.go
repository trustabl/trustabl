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

func TestResolveEdges_ADKHostedToolAndFunctionTool(t *testing.T) {
	src := `from google.adk.agents import LlmAgent
from google.adk.tools import FunctionTool, BashTool

def get_weather(city: str) -> str:
    """Look up the weather."""
    return "sunny"

root = LlmAgent(
    name="root",
    tools=[FunctionTool(get_weather), BashTool()],
)
`
	pf := parsePyFile(t, "main.py", src)
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverADKTools([]analysis.ParsedFile{pf}),
		Agents: analysis.DiscoverADKAgents([]analysis.ParsedFile{pf}),
	}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 {
		t.Errorf("HostedToolRefs: got %d, want 1", len(a.HostedToolRefs))
	} else if a.HostedToolRefs[0].Class != "BashTool" {
		t.Errorf("HostedToolRef class: got %q, want BashTool", a.HostedToolRefs[0].Class)
	}
	if len(a.ToolRefs) != 1 {
		t.Errorf("ToolRefs: got %d, want 1", len(a.ToolRefs))
	} else if a.ToolRefs[0].Resolved == nil ||
		a.ToolRefs[0].Resolved.Name != "get_weather" {
		t.Errorf("ToolRefs[0]: not resolved to get_weather")
	}

	if len(inv.HostedTools) != 1 {
		t.Errorf("inv.HostedTools: got %d, want 1", len(inv.HostedTools))
	} else if inv.HostedTools[0].SDK != models.SDKGoogleADK {
		t.Errorf("HostedTool SDK: got %q, want google_adk", inv.HostedTools[0].SDK)
	}
}

func TestResolveEdges_ADKSubAgents(t *testing.T) {
	src := `from google.adk.agents import LlmAgent, SequentialAgent

child = LlmAgent(name="child")
parent = SequentialAgent(name="parent", sub_agents=[child])
`
	pf := parsePyFile(t, "main.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	var parent *models.AgentDef
	for i := range inv.Agents {
		if inv.Agents[i].Name == "parent" {
			parent = &inv.Agents[i]
		}
	}
	if parent == nil {
		t.Fatal("parent agent not found")
	}
	if len(parent.HandoffRefs) != 1 {
		t.Fatalf("parent.HandoffRefs: got %d, want 1", len(parent.HandoffRefs))
	}
	if parent.HandoffRefs[0].Resolved == nil ||
		parent.HandoffRefs[0].Resolved.Name != "child" {
		t.Errorf("HandoffRefs[0] not resolved to child")
	}
}

func TestResolveEdges_ADKAgentToolNoTransitive(t *testing.T) {
	src := `from google.adk.agents import LlmAgent
from google.adk.tools import AgentTool

inner = LlmAgent(name="inner")
outer = LlmAgent(name="outer", tools=[AgentTool(inner)])
`
	pf := parsePyFile(t, "main.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	var outer *models.AgentDef
	for i := range inv.Agents {
		if inv.Agents[i].Name == "outer" {
			outer = &inv.Agents[i]
		}
	}
	if outer == nil {
		t.Fatal("outer agent not found")
	}
	if len(outer.HostedToolRefs) != 1 || outer.HostedToolRefs[0].Class != "AgentTool" {
		t.Errorf("outer HostedToolRefs: want one AgentTool, got %#v", outer.HostedToolRefs)
	}
}

func TestResolveEdges_ADKSubAgentsByVarName(t *testing.T) {
	// Real-world shape: the Python variable name differs from the name= literal.
	// sub_agents=[greeter] references the variable, not the name= value, so
	// resolution must key on the assignment-target variable.
	src := `from google.adk.agents import LlmAgent, SequentialAgent
from google.adk.tools import BashTool

greeter = LlmAgent(name="greeting_agent", tools=[BashTool()])
root = SequentialAgent(name="root", sub_agents=[greeter])
`
	pf := parsePyFile(t, "main.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverADKAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	var root, greeter *models.AgentDef
	for i := range inv.Agents {
		switch inv.Agents[i].Name {
		case "root":
			root = &inv.Agents[i]
		case "greeting_agent":
			greeter = &inv.Agents[i]
		}
	}
	if root == nil || greeter == nil {
		t.Fatalf("expected both agents discovered; got %+v", inv.Agents)
	}
	if len(root.HandoffRefs) != 1 {
		t.Fatalf("root.HandoffRefs: got %d, want 1", len(root.HandoffRefs))
	}
	if root.HandoffRefs[0].Resolved == nil ||
		root.HandoffRefs[0].Resolved.Name != "greeting_agent" {
		t.Errorf("sub_agents=[greeter] did not resolve to the greeting_agent def: %+v", root.HandoffRefs[0])
	}
}
