package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A raw LangGraph graph built imperatively (StateGraph -> add_node / add_edge ->
// compile, spread across many separate call sites) must be discovered as a
// single StateGraph AgentDef. This is exactly the pattern the single-call
// create_react_agent / create_agent / AgentExecutor discovery misses, and the
// reason a hand-wired graph reported "no entities found".
func TestLangGraph_RawStateGraphDiscovered(t *testing.T) {
	src := `from langgraph.graph import StateGraph, START, END

builder = StateGraph(AgentState)
builder.add_node("model", call_model)
builder.add_node("tools", tool_node)
builder.add_edge(START, "model")
builder.add_conditional_edges("model", should_continue)
builder.add_edge("tools", "model")
app = builder.compile()
`
	pf := parsePyFile(t, "graph.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKLangChain {
		t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKLangChain)
	}
	if a.Class != "StateGraph" {
		t.Errorf("Class: got %q, want StateGraph", a.Class)
	}
	if a.VarName != "builder" {
		t.Errorf("VarName: got %q, want builder", a.VarName)
	}
	if a.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", a.Language)
	}
}

// The import gate must keep an unrelated class named StateGraph (in a file that
// does NOT import langgraph) from being discovered as an agent.
func TestLangGraph_GateExcludesNonLangGraph(t *testing.T) {
	src := `from mypackage import StateGraph

builder = StateGraph(Foo)
app = builder.compile()
`
	pf := parsePyFile(t, "other.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("non-langgraph StateGraph should not be discovered; got %+v", agents)
	}
}

// A StateGraph that wires a dangerous built-in (PythonREPLTool) through a
// ToolNode must resolve it as a HostedToolRef on the graph agent, so agent-scope
// rules (LC-101) can flag the code-execution reach. The tools live in a
// ToolNode([...]) call, not a tools= kwarg.
func TestLangGraph_ToolNodeDangerousToolResolved(t *testing.T) {
	src := `from langgraph.graph import StateGraph
from langgraph.prebuilt import ToolNode
from langchain_experimental.tools import PythonREPLTool

builder = StateGraph(AgentState)
tool_node = ToolNode([PythonREPLTool()])
builder.add_node("tools", tool_node)
app = builder.compile()
`
	pf := parsePyFile(t, "danger_graph.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 || a.HostedToolRefs[0].Class != "PythonREPLTool" {
		t.Fatalf("StateGraph HostedToolRefs: got %+v, want [PythonREPLTool]", a.HostedToolRefs)
	}
	if len(inv.HostedTools) != 1 || inv.HostedTools[0].SDK != models.SDKLangChain {
		t.Errorf("inv.HostedTools: got %+v, want one with SDK=langchain", inv.HostedTools)
	}
}

// The common variable-indirected form: tools = [...]; ToolNode(tools). The list
// assignment must be resolved so the dangerous tool is still attached.
func TestLangGraph_ToolNodeViaVariableResolved(t *testing.T) {
	src := `from langgraph.graph import StateGraph
from langgraph.prebuilt import ToolNode
from langchain_experimental.tools import PythonREPLTool

tools = [PythonREPLTool()]
tool_node = ToolNode(tools)
builder = StateGraph(AgentState)
app = builder.compile()
`
	pf := parsePyFile(t, "var_graph.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 || len(inv.Agents[0].HostedToolRefs) != 1 || inv.Agents[0].HostedToolRefs[0].Class != "PythonREPLTool" {
		t.Fatalf("ToolNode(tools) variable form not resolved; got %+v", inv.Agents)
	}
}

// The same via llm.bind_tools([...]).
func TestLangGraph_BindToolsDangerousToolResolved(t *testing.T) {
	src := `from langgraph.graph import StateGraph
from langchain_experimental.tools import ShellTool

llm = ChatOpenAI()
model = llm.bind_tools([ShellTool()])
builder = StateGraph(AgentState)
app = builder.compile()
`
	pf := parsePyFile(t, "bind.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 || len(inv.Agents[0].HostedToolRefs) != 1 || inv.Agents[0].HostedToolRefs[0].Class != "ShellTool" {
		t.Fatalf("bind_tools ShellTool not resolved onto the StateGraph; got %+v", inv.Agents)
	}
}

// A StateGraph with only a benign tool must carry no dangerous HostedToolRefs
// (no false LC-101 fire).
func TestLangGraph_BenignToolNodeNoHostedRef(t *testing.T) {
	src := `from langgraph.graph import StateGraph
from langgraph.prebuilt import ToolNode

builder = StateGraph(AgentState)
tool_node = ToolNode([search_tool])
app = builder.compile()
`
	pf := parsePyFile(t, "benign.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("want 1 agent")
	}
	if len(inv.Agents[0].HostedToolRefs) != 0 {
		t.Errorf("benign ToolNode must not produce hosted refs; got %+v", inv.Agents[0].HostedToolRefs)
	}
}

// A bare `Graph()` call from an unrelated package (networkx / rdflib / graphviz)
// in a file that merely also imports langgraph must NOT be discovered as a
// StateGraph agent. The builder callee is bound to its langchain/langgraph
// import origin, not matched by bare name.
func TestLangGraph_BareGraphFromUnrelatedPackageExcluded(t *testing.T) {
	src := `from langgraph.graph import StateGraph
import networkx as nx
from networkx import Graph

g = Graph()
h = nx.Graph()
`
	pf := parsePyFile(t, "nx.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("unrelated Graph() must not be discovered; got %+v", agents)
	}
}

// A module-qualified constructor (import langgraph.graph as lg; lg.StateGraph)
// must be discovered — matched on the trailing segment, bound to the langgraph
// import alias.
func TestLangGraph_QualifiedStateGraphDiscovered(t *testing.T) {
	src := `import langgraph.graph as lg

app = lg.StateGraph(AgentState)
`
	pf := parsePyFile(t, "qualified.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("qualified lg.StateGraph not discovered; got %d", len(agents))
	}
	if agents[0].Class != "StateGraph" {
		t.Errorf("Class: got %q, want StateGraph", agents[0].Class)
	}
}

// A `StateGraph` imported from an unrelated package, in a file that also imports
// a langchain provider package, must NOT be discovered — the constructor name is
// bound to its actual import origin, not the file-level gate.
func TestLangGraph_StateGraphFromUnrelatedPackageExcluded(t *testing.T) {
	src := `from langchain_openai import ChatOpenAI
from mypackage import StateGraph

b = StateGraph(X)
app = b.compile()
`
	pf := parsePyFile(t, "foreign.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("StateGraph from a non-langgraph package must not be discovered; got %+v", agents)
	}
}

// The compiled-graph terminus carries the security-relevant kwargs (a
// human-in-the-loop interrupt, a checkpointer). They must be captured onto the
// StateGraph AgentDef so rules can read them, even though .compile() is a
// separate call site from the StateGraph(...) constructor.
func TestLangGraph_CompileKwargsCaptured(t *testing.T) {
	src := `from langgraph.graph import StateGraph

builder = StateGraph(AgentState)
builder.add_node("tools", tool_node)
app = builder.compile(checkpointer=memory, interrupt_before=["tools"])
`
	pf := parsePyFile(t, "hitl.py", src)
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.Kwargs == nil || a.Kwargs.Children["interrupt_before"] == nil {
		t.Errorf("compile() interrupt_before not captured onto the agent: %+v", a.Kwargs)
	}
	if a.Kwargs == nil || a.Kwargs.Children["checkpointer"] == nil {
		t.Errorf("compile() checkpointer not captured onto the agent: %+v", a.Kwargs)
	}
}
