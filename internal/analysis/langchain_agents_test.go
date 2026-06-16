package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestLangChainAgent_CreateReactAgentPositionalTools(t *testing.T) {
	src := `from langgraph.prebuilt import create_react_agent

agent = create_react_agent(model, [search, calculator], prompt="Be helpful.")
`
	pf := parsePyFile(t, "agent.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKLangChain {
		t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKLangChain)
	}
	if a.Class != "ReactAgent" {
		t.Errorf("Class: got %q, want ReactAgent", a.Class)
	}
	if a.VarName != "agent" {
		t.Errorf("VarName: got %q, want agent", a.VarName)
	}
	// Positional tools (index 1, after the model) must be captured so edge
	// resolution and hosted-tool detection can see them.
	tools := a.Kwargs.Children["tools"]
	if tools == nil || tools.Value == nil || tools.Value.Kind != models.ExprList {
		t.Fatalf("positional tools not captured as a list: %+v", a.Kwargs)
	}
}

func TestLangChainAgent_CreateAgentKwargs(t *testing.T) {
	src := `from langchain.agents import create_agent

agent = create_agent(model="openai:gpt-4o", tools=[search], system_prompt="You help.")
`
	pf := parsePyFile(t, "ca.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.Class != "CreateAgent" {
		t.Errorf("Class: got %q, want CreateAgent", a.Class)
	}
	if a.Kwargs.Children["system_prompt"] == nil {
		t.Errorf("system_prompt kwarg not captured")
	}
}

func TestLangChainAgent_AgentExecutor(t *testing.T) {
	src := `from langchain.agents import AgentExecutor

ex = AgentExecutor(agent=a, tools=[search], max_iterations=5)
`
	pf := parsePyFile(t, "ex.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.Class != "AgentExecutor" {
		t.Errorf("Class: got %q, want AgentExecutor", a.Class)
	}
	if a.Kwargs.Children["max_iterations"] == nil {
		t.Errorf("max_iterations kwarg not captured")
	}
}

func TestLangChainAgent_GateExcludesNonLangChain(t *testing.T) {
	src := `def create_agent(**kw):
    return kw

x = create_agent(model="m")
`
	pf := parsePyFile(t, "no_lc.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("non-langchain create_agent should not be discovered; got %+v", agents)
	}
}

// A scalar positional (a prompt string) at index 1 must NOT be captured as the
// synthetic tools kwarg — only list/identifier/call shapes that could be a tools
// collection.
func TestLangChainAgent_PositionalPromptNotCapturedAsTools(t *testing.T) {
	src := `from langgraph.prebuilt import create_react_agent

agent = create_react_agent(model, "Be a helpful assistant.")
`
	pf := parsePyFile(t, "promptpos.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	if agents[0].Kwargs != nil && agents[0].Kwargs.Children["tools"] != nil {
		t.Errorf("positional prompt string must not be captured as tools: %+v", agents[0].Kwargs)
	}
}

// A module-qualified / aliased agent constructor (import langchain.agents as la;
// la.AgentExecutor(...)) must be discovered — matched on the trailing segment,
// bound to the langchain import alias.
func TestLangChainAgent_QualifiedExecutorDiscovered(t *testing.T) {
	src := `import langchain.agents as la

ex = la.AgentExecutor(agent=a, tools=[search], max_iterations=5)
`
	pf := parsePyFile(t, "qual.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 || agents[0].Class != "AgentExecutor" {
		t.Fatalf("qualified la.AgentExecutor not discovered as AgentExecutor; got %+v", agents)
	}
}

// A locally-defined create_agent in a file that also imports a langchain provider
// package must NOT be discovered — the constructor name is bound to its import
// origin, and a local def is not an import.
func TestLangChainAgent_LocalShadowExcluded(t *testing.T) {
	src := `from langchain_openai import ChatOpenAI

def create_agent(**kw):
    return kw

x = create_agent(model="m")
`
	pf := parsePyFile(t, "shadow.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("locally-defined create_agent must not be discovered; got %+v", agents)
	}
}

// The AgentExecutor.from_agent_and_tools classmethod must still resolve to
// AgentExecutor after the import-binding refactor.
func TestLangChainAgent_FromAgentAndToolsClassmethod(t *testing.T) {
	src := `from langchain.agents import AgentExecutor

ex = AgentExecutor.from_agent_and_tools(agent=a, tools=[search])
`
	pf := parsePyFile(t, "fromagent.py", src)
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 || agents[0].Class != "AgentExecutor" {
		t.Fatalf("AgentExecutor.from_agent_and_tools not discovered; got %+v", agents)
	}
}

// A dangerous built-in (PythonREPLTool) passed positionally to create_react_agent
// must resolve to a HostedToolDef edge with SDK=langchain (not fall through to
// the OpenAI classifier or to an External ToolRef).
func TestLangChainAgent_HostedToolResolution(t *testing.T) {
	src := `from langgraph.prebuilt import create_react_agent
from langchain_experimental.tools import PythonREPLTool

agent = create_react_agent(model, [PythonREPLTool()])
`
	pf := parsePyFile(t, "danger.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 || a.HostedToolRefs[0].Class != "PythonREPLTool" {
		t.Fatalf("HostedToolRefs: got %+v, want [PythonREPLTool]", a.HostedToolRefs)
	}
	if len(inv.HostedTools) != 1 || inv.HostedTools[0].SDK != models.SDKLangChain {
		t.Errorf("inv.HostedTools: got %+v, want one with SDK=langchain", inv.HostedTools)
	}
}

// A LangChain tool referenced by identifier in an agent's tools list resolves to
// the discovered ToolDef.
func TestLangChainAgent_ResolvesToolEdges(t *testing.T) {
	src := `from langchain_core.tools import tool
from langgraph.prebuilt import create_react_agent

@tool
def search(q: str) -> str:
    """Search."""
    return q

agent = create_react_agent(model, [search])
`
	pf := parsePyFile(t, "wired.py", src)
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}),
		Agents: analysis.DiscoverLangChainAgents([]analysis.ParsedFile{pf}),
	}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	a := inv.Agents[0]
	if len(a.ToolRefs) != 1 {
		t.Fatalf("ToolRefs: got %+v, want 1", a.ToolRefs)
	}
	if a.ToolRefs[0].Resolved == nil || a.ToolRefs[0].Resolved.Name != "search" {
		t.Errorf("tool edge not resolved to 'search': %+v", a.ToolRefs[0])
	}
}
