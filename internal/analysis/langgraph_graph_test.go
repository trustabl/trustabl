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
