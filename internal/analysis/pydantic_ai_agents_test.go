package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A Pydantic AI Agent(...) in a file that imports pydantic_ai is emitted as one
// AgentDef with SDK=SDKPydanticAI, the normalized Class "PydanticAgent", and its
// constructor kwargs captured so the agent-scope rules (PYD-101/105) can read
// output_type / end_strategy.
func TestPydanticAIAgent_ConstructorCaptured(t *testing.T) {
	src := `from pydantic_ai import Agent

agent = Agent(
    "openai:gpt-4o",
    name="assistant",
    output_type=str,
    end_strategy="exhaustive",
)
`
	pf := parsePyFile(t, "agent.py", src)
	agents := analysis.DiscoverPydanticAIAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKPydanticAI {
		t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKPydanticAI)
	}
	if a.Class != "PydanticAgent" {
		t.Errorf("Class: got %q, want PydanticAgent", a.Class)
	}
	if a.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", a.Language)
	}
	if a.VarName != "agent" {
		t.Errorf("VarName: got %q, want agent", a.VarName)
	}
	if a.Name != "assistant" {
		t.Errorf("Name: got %q, want assistant", a.Name)
	}
	if kw := a.Kwargs.Children["end_strategy"]; kw == nil || kw.Value == nil || kw.Value.Text != `"exhaustive"` {
		t.Fatalf("end_strategy kwarg not captured: %+v", a.Kwargs)
	}
}

// Collision guard: the class name `Agent` is shared with OpenAI/ADK/CrewAI, so
// an `Agent(...)` in a file that does NOT import pydantic_ai must not be
// discovered as a PydanticAgent. DiscoverPydanticAIAgents is import-gated, so it
// emits nothing here.
func TestPydanticAIAgent_GateExcludesUnimported(t *testing.T) {
	src := `from agents import Agent

agent = Agent(name="ops", instructions="Run ops.", model="gpt-4")
`
	pf := parsePyFile(t, "oai.py", src)
	if pyd := analysis.DiscoverPydanticAIAgents([]analysis.ParsedFile{pf}); len(pyd) != 0 {
		t.Errorf("non-pydantic Agent must not be a PydanticAgent; got %+v", pyd)
	}
	// And the OpenAI discovery still claims it as an OpenAI agent.
	oai := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(oai) != 1 || oai[0].SDK != models.SDKOpenAIAgents {
		t.Errorf("OpenAI Agent must stay SDKOpenAIAgents; got %+v", oai)
	}
}

// NativeTool-unwrap (modern form): a Pydantic agent wired with
// capabilities=[NativeTool(CodeExecutionTool())] must surface a HostedToolRef
// whose Class is the UNWRAPPED inner class CodeExecutionTool (not NativeTool),
// stamped SDKPydanticAI. This is the load-bearing unwrap in
// classifyPydanticAIHostedToolCall.
func TestResolveEdges_PydanticNativeToolUnwrap(t *testing.T) {
	src := `from pydantic_ai import Agent, NativeTool, CodeExecutionTool

agent = Agent(
    "openai:gpt-4o",
    capabilities=[NativeTool(CodeExecutionTool())],
)
`
	pf := parsePyFile(t, "native.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverPydanticAIAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 {
		t.Fatalf("HostedToolRefs: got %d, want 1 (%+v)", len(a.HostedToolRefs), a.HostedToolRefs)
	}
	if a.HostedToolRefs[0].Class != "CodeExecutionTool" {
		t.Errorf("HostedToolRef class: got %q, want CodeExecutionTool (NativeTool wrapper must be unwrapped)", a.HostedToolRefs[0].Class)
	}
	if len(inv.HostedTools) != 1 || inv.HostedTools[0].SDK != models.SDKPydanticAI {
		t.Errorf("inv.HostedTools: got %+v, want one SDKPydanticAI entry", inv.HostedTools)
	}
}

// Legacy form: builtin_tools=[WebFetchTool()] (no NativeTool wrapper) is also
// classified, proving both kwargs are scanned and the unwrap is optional.
func TestResolveEdges_PydanticBuiltinToolsLegacy(t *testing.T) {
	src := `from pydantic_ai import Agent, WebFetchTool

agent = Agent(
    "openai:gpt-4o",
    builtin_tools=[WebFetchTool()],
)
`
	pf := parsePyFile(t, "legacy.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverPydanticAIAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 || a.HostedToolRefs[0].Class != "WebFetchTool" {
		t.Errorf("HostedToolRefs: got %+v, want one WebFetchTool", a.HostedToolRefs)
	}
}
