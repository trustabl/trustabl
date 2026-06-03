package analysis_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parsePyFile(t *testing.T, path, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return analysis.ParsedFile{RelPath: path, Source: []byte(src), Tree: tree}
}

// ─── DiscoverAgents ─────────────────────────────────────────────────────────

func TestDiscoverAgents_FindsOpenAIAgent(t *testing.T) {
	src := `
from agents import Agent

agent = Agent(
    name="ops",
    instructions="Run ops tasks.",
    model="gpt-4",
)
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK = %v, want SDKOpenAIAgents", a.SDK)
	}
	if a.Class != "Agent" {
		t.Errorf("Class = %v, want Agent", a.Class)
	}
	if a.Name != "ops" {
		t.Errorf("Name = %v, want ops", a.Name)
	}
	if a.Kwargs == nil || a.Kwargs.Children["model"] == nil {
		t.Errorf("expected model kwarg captured")
	}
}

func TestDiscoverAgents_FindsSandboxAgent(t *testing.T) {
	src := `
from agents import SandboxAgent
agent = SandboxAgent(name="sb")
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 || agents[0].Class != "SandboxAgent" {
		t.Fatalf("expected SandboxAgent, got %+v", agents)
	}
}

func TestDiscoverAgents_FindsClaudeAgentDefinition(t *testing.T) {
	src := `
from claude_agent_sdk import AgentDefinition
agent = AgentDefinition(name="claude")
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 || agents[0].SDK != models.SDKClaudeAgentSDK {
		t.Fatalf("expected Claude agent, got %+v", agents)
	}
}

func TestDiscoverAgents_NameFromDictKey(t *testing.T) {
	src := `
from claude_agent_sdk import AgentDefinition
agents = {
    "researcher": AgentDefinition(description="d", tools=["WebSearch"], model="haiku"),
    "data-analyst": AgentDefinition(description="d", tools=["Bash"], model="haiku"),
}
`
	pf := parsePyFile(t, "main.py", src)
	got := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %d: %+v", len(got), got)
	}
	names := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !names["researcher"] || !names["data-analyst"] {
		t.Errorf("expected names researcher + data-analyst, got %q and %q", got[0].Name, got[1].Name)
	}
}

func TestDiscoverAgents_NameFromAssignment(t *testing.T) {
	src := `
from claude_agent_sdk import AgentDefinition
researcher = AgentDefinition(description="d", tools=["WebSearch"])
`
	pf := parsePyFile(t, "main.py", src)
	got := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(got) != 1 || got[0].Name != "researcher" {
		t.Fatalf("expected name 'researcher', got %+v", got)
	}
}

func TestDiscoverAgents_NameKwargWinsOverDictKey(t *testing.T) {
	src := `
from agents import Agent
mapping = {"key_name": Agent(name="kwarg_name")}
`
	pf := parsePyFile(t, "main.py", src)
	got := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(got) != 1 || got[0].Name != "kwarg_name" {
		t.Fatalf("expected name= kwarg to win, got %+v", got)
	}
}

func TestDiscoverAgents_SkipsUnrelatedCalls(t *testing.T) {
	src := `
def foo():
    bar(name="x")
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

// ─── DiscoverGuardrails ──────────────────────────────────────────────────────

func TestDiscoverGuardrails_FindsInputAndOutput(t *testing.T) {
	src := `
from agents import input_guardrail, output_guardrail, GuardrailFunctionOutput

@input_guardrail
def check_input(ctx, agent, input):
    return GuardrailFunctionOutput(output_info=None, tripwire_triggered=False)

@output_guardrail
def check_output(ctx, agent, output):
    return GuardrailFunctionOutput(output_info=None, tripwire_triggered=False)
`
	pf := parsePyFile(t, "g.py", src)
	gs := analysis.DiscoverGuardrails([]analysis.ParsedFile{pf})
	if len(gs) != 2 {
		t.Fatalf("expected 2 guardrails, got %d", len(gs))
	}
	var inputCount, outputCount int
	for _, g := range gs {
		if g.Kind == models.GuardrailInput {
			inputCount++
		}
		if g.Kind == models.GuardrailOutput {
			outputCount++
		}
	}
	if inputCount != 1 || outputCount != 1 {
		t.Errorf("expected 1 input + 1 output, got %d input, %d output", inputCount, outputCount)
	}
}

func TestDiscoverGuardrails_SkipsUndecoratedFunctions(t *testing.T) {
	src := `
def plain(ctx, agent, input):
    return None
`
	pf := parsePyFile(t, "g.py", src)
	gs := analysis.DiscoverGuardrails([]analysis.ParsedFile{pf})
	if len(gs) != 0 {
		t.Errorf("expected 0 guardrails, got %d", len(gs))
	}
}

// ─── DiscoverSessions ────────────────────────────────────────────────────────

func TestDiscoverSessions(t *testing.T) {
	src := `
from agents import SQLiteSession
session = SQLiteSession("convo")
`
	pf := parsePyFile(t, "s.py", src)
	ss := analysis.DiscoverSessions([]analysis.ParsedFile{pf})
	if len(ss) != 1 || ss[0].Class != "SQLiteSession" {
		t.Fatalf("expected SQLiteSession, got %+v", ss)
	}
}

func TestDiscoverSessions_SkipsUnimportedClasses(t *testing.T) {
	src := `
session = SQLiteSession("convo")
`
	pf := parsePyFile(t, "s.py", src)
	ss := analysis.DiscoverSessions([]analysis.ParsedFile{pf})
	if len(ss) != 0 {
		t.Errorf("expected 0 sessions (not imported), got %d", len(ss))
	}
}

// ─── EndLine attribution ─────────────────────────────────────────────────────

func TestGuardrailDef_EndLine(t *testing.T) {
	// Line 1:  (empty — leading newline)
	// Line 2:  from agents import input_guardrail
	// Line 3:  (empty)
	// Line 4:  @input_guardrail
	// Line 5:  def my_guard(
	// Line 6:      ctx,
	// Line 7:      agent,
	// Line 8:      inp,
	// Line 9:  ):
	// Line 10:     return True
	src := `
from agents import input_guardrail

@input_guardrail
def my_guard(
    ctx,
    agent,
    inp,
):
    return True
`
	pf := parsePyFile(t, "g.py", src)
	gs := analysis.DiscoverGuardrails([]analysis.ParsedFile{pf})
	if len(gs) != 1 {
		t.Fatalf("expected 1 guardrail, got %d", len(gs))
	}
	g := gs[0]
	if g.EndLine == 0 {
		t.Fatalf("EndLine is 0 — not populated")
	}
	if g.EndLine <= g.Line {
		t.Errorf("EndLine (%d) must be > Line (%d) for a multi-line function", g.EndLine, g.Line)
	}
	// def starts on line 5, last line of function body is line 10
	if g.Line != 5 {
		t.Errorf("Line = %d, want 5", g.Line)
	}
	if g.EndLine != 10 {
		t.Errorf("EndLine = %d, want 10", g.EndLine)
	}
}

func TestSessionUse_EndLine(t *testing.T) {
	// Line 1:  (empty — leading newline)
	// Line 2:  from agents import SQLiteSession
	// Line 3:  (empty)
	// Line 4:  session = SQLiteSession(
	// Line 5:      "conv-123",
	// Line 6:      "sessions.db",
	// Line 7:  )
	src := `
from agents import SQLiteSession

session = SQLiteSession(
    "conv-123",
    "sessions.db",
)
`
	pf := parsePyFile(t, "s.py", src)
	ss := analysis.DiscoverSessions([]analysis.ParsedFile{pf})
	if len(ss) != 1 {
		t.Fatalf("expected 1 session, got %d", len(ss))
	}
	s := ss[0]
	if s.EndLine == 0 {
		t.Fatalf("EndLine is 0 — not populated")
	}
	if s.EndLine <= s.Line {
		t.Errorf("EndLine (%d) must be > Line (%d) for a multi-line call", s.EndLine, s.Line)
	}
	// call starts on line 4, closing ) is on line 7
	if s.Line != 4 {
		t.Errorf("Line = %d, want 4", s.Line)
	}
	if s.EndLine != 7 {
		t.Errorf("EndLine = %d, want 7", s.EndLine)
	}
}

// ─── Decorator kwargs capture ─────────────────────────────────────────────────

func TestDiscoverTools_CapturesDecoratorKwargs(t *testing.T) {
	src := `
from agents import function_tool

@function_tool(strict_mode=False, name_override="my_tool")
def do_thing(x: str) -> str:
    """Do a thing."""
    return x
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := models.ScanManifest{RepoRoot: dir, PythonFiles: []string{"t.py"}}
	tools, _, err := analysis.DiscoverTools(manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Config["strict_mode"] != "False" {
		t.Errorf("strict_mode = %q, want False", tools[0].Config["strict_mode"])
	}
	if tools[0].Config["name_override"] != `"my_tool"` {
		t.Errorf("name_override = %q, want %q", tools[0].Config["name_override"], `"my_tool"`)
	}
}

// ─── ResolveEdges — in-file ───────────────────────────────────────────────────

func TestResolveEdges_InFileTool(t *testing.T) {
	src := `
from agents import Agent, function_tool

@function_tool
def my_tool(x: str) -> str:
    """A tool."""
    return x

agent = Agent(name="a", tools=[my_tool])
`
	pf := parsePyFile(t, "main.py", src)
	parsed := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(parsed),
		Agents: analysis.DiscoverAgents(parsed),
	}
	analysis.ResolveEdges(&inv, parsed)
	if len(inv.Agents) != 1 {
		t.Fatalf("agents = %d", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.ToolRefs) != 1 || a.ToolRefs[0].Name != "my_tool" {
		t.Fatalf("ToolRefs = %+v", a.ToolRefs)
	}
	if a.ToolRefs[0].External {
		t.Error("expected ToolRef.External = false (same file)")
	}
	if a.ToolRefs[0].Resolved == nil {
		t.Error("expected ToolRef.Resolved to be non-nil")
	}
}

// ─── ResolveEdges — cross-module ──────────────────────────────────────────────

func TestResolveEdges_CrossModuleTool(t *testing.T) {
	toolFile := parsePyFile(t, "tools.py", `
from agents import function_tool

@function_tool
def my_tool(x: str) -> str:
    """A tool."""
    return x
`)
	agentFile := parsePyFile(t, "agent.py", `
from agents import Agent
from tools import my_tool

agent = Agent(name="a", tools=[my_tool])
`)
	parsed := []analysis.ParsedFile{toolFile, agentFile}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(parsed),
		Agents: analysis.DiscoverAgents(parsed),
	}
	analysis.ResolveEdges(&inv, parsed)
	if len(inv.Agents[0].ToolRefs) != 1 {
		t.Fatal("expected one tool ref")
	}
	ref := inv.Agents[0].ToolRefs[0]
	if ref.External {
		t.Error("expected cross-module resolution, got External=true")
	}
	if ref.Resolved == nil || ref.Resolved.FilePath != "tools.py" {
		t.Errorf("expected resolved to tools.py, got %+v", ref.Resolved)
	}
}

// ─── ResolveEdges — opaque ────────────────────────────────────────────────────

func TestResolveEdges_OpaqueKwargsUnpack(t *testing.T) {
	src := `
from agents import Agent
config = {"name": "x", "tools": []}
agent = Agent(**config)
`
	pf := parsePyFile(t, "main.py", src)
	agents := analysis.DiscoverAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 || !agents[0].Opaque {
		t.Fatalf("expected Opaque=true, got %+v", agents)
	}
}

func TestResolveEdges_OpaqueToolsFactory(t *testing.T) {
	src := `
from agents import Agent

def get_tools(): return []

agent = Agent(name="x", tools=get_tools())
`
	pf := parsePyFile(t, "main.py", src)
	parsed := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(parsed),
		Agents: analysis.DiscoverAgents(parsed),
	}
	analysis.ResolveEdges(&inv, parsed)
	if !inv.Agents[0].Opaque {
		t.Errorf("expected Opaque=true after ResolveEdges saw tools=get_tools(), got false")
	}
}

// ─── ResolveEdges — external ──────────────────────────────────────────────────

func TestResolveEdges_ExternalTool(t *testing.T) {
	src := `
from agents import Agent
from third_party import some_tool

agent = Agent(name="x", tools=[some_tool])
`
	pf := parsePyFile(t, "main.py", src)
	parsed := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(parsed),
		Agents: analysis.DiscoverAgents(parsed),
	}
	analysis.ResolveEdges(&inv, parsed)
	if len(inv.Agents[0].ToolRefs) != 1 || !inv.Agents[0].ToolRefs[0].External {
		t.Errorf("expected External=true for unresolvable tool, got %+v", inv.Agents[0].ToolRefs)
	}
}

// ─── ResolveEdges — determinism ───────────────────────────────────────────────

func TestResolveEdges_DeterministicSameName(t *testing.T) {
	fileA := parsePyFile(t, "a.py", `
from agents import function_tool
@function_tool
def shared(x: str) -> str:
    """Shared name."""
    return x
`)
	fileB := parsePyFile(t, "b.py", `
from agents import function_tool
@function_tool
def shared(x: str) -> str:
    """Shared name."""
    return x
`)
	agentFile := parsePyFile(t, "agent.py", `
from agents import Agent
from a import shared

agent = Agent(name="x", tools=[shared])
`)
	parsed := []analysis.ParsedFile{fileA, fileB, agentFile}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(parsed),
		Agents: analysis.DiscoverAgents(parsed),
	}
	analysis.ResolveEdges(&inv, parsed)
	if len(inv.Agents[0].ToolRefs) != 1 {
		t.Fatal("expected one tool ref")
	}
	if inv.Agents[0].ToolRefs[0].Resolved == nil ||
		inv.Agents[0].ToolRefs[0].Resolved.FilePath != "a.py" {
		t.Errorf("expected resolved to a.py, got %+v", inv.Agents[0].ToolRefs[0].Resolved)
	}
}

// ─── ResolveEdges — TS OpenAI VarName resolution ──────────────────────────────

func TestResolveEdges_TSToolByVarName(t *testing.T) {
	inv := &models.RepoInventory{
		Tools: []models.ToolDef{{
			Name:     "sum",
			VarName:  "computeSum",
			Kind:     models.KindOpenAITool,
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 1},
		}},
		Agents: []models.AgentDef{{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 10},
			ToolRefs: []models.ToolRef{{Name: "computeSum"}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	got := inv.Agents[0].ToolRefs[0]
	if got.Resolved == nil {
		t.Errorf("expected ToolRef to resolve via VarName, got External=%v", got.External)
	} else if got.Resolved.Name != "sum" {
		t.Errorf("resolved to wrong tool: %+v", got.Resolved)
	}
}

func TestResolveEdges_PythonToolByName_BackwardCompat(t *testing.T) {
	inv := &models.RepoInventory{
		Tools: []models.ToolDef{{
			Name:     "myTool",
			Kind:     models.KindOpenAITool,
			Language: models.LanguagePython,
			Location: models.Location{FilePath: "src/a.py", Line: 1},
		}},
		Agents: []models.AgentDef{{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Location: models.Location{FilePath: "src/a.py", Line: 10},
			ToolRefs: []models.ToolRef{{Name: "myTool"}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	if inv.Agents[0].ToolRefs[0].Resolved == nil {
		t.Errorf("Python case should still resolve by Name")
	}
}

func TestResolveEdges_TSMCPByVarName(t *testing.T) {
	inv := &models.RepoInventory{
		MCPServers: []models.MCPServerDef{{
			Class:     "MCPServerStdio",
			VarName:   "fsServer",
			Transport: "stdio",
			SDK:       models.SDKOpenAIAgents,
			Language:  models.LanguageTypeScript,
			Location:  models.Location{FilePath: "src/a.ts", Line: 1},
		}},
		Agents: []models.AgentDef{{
			SDK:           models.SDKOpenAIAgents,
			Class:         "Agent",
			Language:      models.LanguageTypeScript,
			Location:      models.Location{FilePath: "src/a.ts", Line: 10},
			MCPServerRefs: []models.MCPServerRef{{Class: "fsServer", DefIndex: -1}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	got := inv.Agents[0].MCPServerRefs[0]
	if got.External || got.Resolved == nil {
		t.Errorf("expected MCPServerRef to resolve via VarName, got %+v", got)
	}
	if got.Resolved.Class != "MCPServerStdio" {
		t.Errorf("wrong class: %q", got.Resolved.Class)
	}
}

func TestResolveEdges_TSGuardrailByVarName(t *testing.T) {
	inv := &models.RepoInventory{
		Guardrails: []models.GuardrailDef{{
			Name:     "block_pii",
			VarName:  "blockPII",
			Kind:     "input",
			Location: models.Location{FilePath: "src/a.ts", Line: 1},
		}},
		Agents: []models.AgentDef{{
			SDK:         models.SDKOpenAIAgents,
			Class:       "Agent",
			Language:    models.LanguageTypeScript,
			Location:    models.Location{FilePath: "src/a.ts", Line: 10},
			InputGuards: []models.GuardrailRef{{Name: "blockPII"}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	got := inv.Agents[0].InputGuards[0]
	if got.External || got.Resolved == nil {
		t.Errorf("expected GuardrailRef to resolve via VarName, got %+v", got)
	}
}

func TestResolveEdges_TSHostedToolAppendedToInventory(t *testing.T) {
	// AgentDef carries a HostedToolRef from discovery; ResolveEdges should
	// append a corresponding HostedToolDef to inv.HostedTools and update
	// DefIndex via the sort permutation.
	inv := &models.RepoInventory{
		Agents: []models.AgentDef{{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguageTypeScript,
			Location:       models.Location{FilePath: "src/a.ts", Line: 10},
			HostedToolRefs: []models.HostedToolRef{{Class: "webSearchTool", DefIndex: -1}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	if len(inv.HostedTools) != 1 {
		t.Fatalf("expected 1 HostedToolDef in inventory, got %d", len(inv.HostedTools))
	}
	if inv.HostedTools[0].Class != "webSearchTool" {
		t.Errorf("wrong hosted-tool class: %q", inv.HostedTools[0].Class)
	}
	if inv.HostedTools[0].SDK != models.SDKOpenAIAgents {
		t.Errorf("wrong SDK: %q", inv.HostedTools[0].SDK)
	}
	if inv.Agents[0].HostedToolRefs[0].Resolved == nil {
		t.Errorf("HostedToolRef should be resolved after edges")
	}
}

func TestResolveEdges_TSHostedToolNoDoubleEmit(t *testing.T) {
	// Regression: TS OpenAI agents must not have their hosted-tool call
	// items re-emitted as External ToolRefs by the Python tools-kwarg block.
	// Discovery pre-populates HostedToolRefs from `tools: [webSearchTool()]`;
	// ResolveEdges must not also emit a ToolRef{Name: "webSearchTool({...})"}.
	inv := &models.RepoInventory{
		Agents: []models.AgentDef{{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 10},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"tools": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprCall, Text: "webSearchTool()"},
				}}},
			}},
			HostedToolRefs: []models.HostedToolRef{{Class: "webSearchTool", DefIndex: -1}},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	if len(inv.Agents[0].ToolRefs) != 0 {
		t.Errorf("ToolRefs should be empty (hosted tool already in HostedToolRefs), got %+v",
			inv.Agents[0].ToolRefs)
	}
	if len(inv.Agents[0].HostedToolRefs) != 1 {
		t.Errorf("HostedToolRefs should have 1 entry, got %+v", inv.Agents[0].HostedToolRefs)
	}
}

func TestResolveEdges_TSOpaqueAgent_StillResolvesPrePopulatedRefs(t *testing.T) {
	// Regression: a spread-Opaque TS agent (`new Agent({...defaults, tools:
	// [webSearchTool()]})`) populates HostedToolRefs/MCPServerRefs/ToolRefs
	// at discovery time even though Opaque=true. ResolveEdges must still
	// run the language-agnostic ref-resolution passes for it — the refs
	// were extracted from explicit syntactic positions before the spread
	// was encountered, so they're trustworthy even when Kwargs aren't.
	inv := &models.RepoInventory{
		Tools: []models.ToolDef{{
			Name:     "sum",
			VarName:  "computeSum",
			Kind:     models.KindOpenAITool,
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 1},
		}},
		MCPServers: []models.MCPServerDef{{
			Class:     "MCPServerStdio",
			VarName:   "fsServer",
			Transport: "stdio",
			SDK:       models.SDKOpenAIAgents,
			Language:  models.LanguageTypeScript,
			Location:  models.Location{FilePath: "src/a.ts", Line: 3},
		}},
		Agents: []models.AgentDef{{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 10},
			Opaque:   true, // spread inside opts
			ToolRefs: []models.ToolRef{{Name: "computeSum"}},
			HostedToolRefs: []models.HostedToolRef{
				{Class: "webSearchTool", DefIndex: -1},
			},
			MCPServerRefs: []models.MCPServerRef{
				{Class: "fsServer", DefIndex: -1},
			},
		}},
	}
	analysis.ResolveEdges(inv, nil)

	a := inv.Agents[0]
	if a.ToolRefs[0].Resolved == nil {
		t.Errorf("ToolRef should be resolved by VarName even for Opaque TS agent, got External=%v",
			a.ToolRefs[0].External)
	}
	if len(inv.HostedTools) != 1 {
		t.Errorf("inv.HostedTools should be materialized from HostedToolRef even for Opaque TS agent, got %d entries",
			len(inv.HostedTools))
	}
	if a.HostedToolRefs[0].Resolved == nil {
		t.Errorf("HostedToolRef should resolve after the post-sort remap even for Opaque TS agent")
	}
	if a.MCPServerRefs[0].External || a.MCPServerRefs[0].Resolved == nil {
		t.Errorf("MCPServerRef should resolve by VarName even for Opaque TS agent, got %+v",
			a.MCPServerRefs[0])
	}
}

func TestResolveEdges_TSADKSubAgentsCamelCase(t *testing.T) {
	// Regression: TS ADK discovery pre-populates HandoffRefs from camelCase
	// subAgents at parse time (ts_adk_agents.go). ResolveEdges must wire
	// those pre-populated refs via same-file Name/VarName lookup — and must
	// NOT re-walk the kwarg to append again (that would double-emit; the
	// scanner_test.go ADK integration test catches that regression).
	inv := &models.RepoInventory{
		Agents: []models.AgentDef{
			{
				SDK:      models.SDKGoogleADK,
				Class:    "LlmAgent",
				Language: models.LanguageTypeScript,
				Location: models.Location{FilePath: "src/a.ts", Line: 5},
				Name:     "writer",
				VarName:  "writer",
			},
			{
				SDK:         models.SDKGoogleADK,
				Class:       "LlmAgent",
				Language:    models.LanguageTypeScript,
				Location:    models.Location{FilePath: "src/a.ts", Line: 12},
				Name:        "researcher",
				VarName:     "researcher",
				HandoffRefs: []models.AgentRef{{Name: "writer"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"subAgents": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
						{Kind: models.ExprNameRef, Text: "writer"},
					}}},
				}},
			},
		},
	}
	analysis.ResolveEdges(inv, nil)
	researcher := inv.Agents[1]
	if len(researcher.HandoffRefs) != 1 {
		t.Fatalf("expected 1 HandoffRef from subAgents (pre-populated, not re-appended), got %d", len(researcher.HandoffRefs))
	}
	ref := researcher.HandoffRefs[0]
	if ref.External || ref.Resolved == nil {
		t.Errorf("HandoffRef should resolve to same-file writer, got %+v", ref)
	}
	if ref.Resolved.Name != "writer" {
		t.Errorf("HandoffRef resolved to wrong agent: %+v", ref.Resolved)
	}
}

func TestResolveEdges_OpenAIPythonHandoffs(t *testing.T) {
	// Regression (TR-146): Python OpenAI agents capture handoffs= in Kwargs but
	// the edge was never turned into HandoffRefs, leaving PredAgentIsSubagentOfAny
	// blind to every Python OpenAI handoff. handoffs=[multiply_agent] must
	// resolve to the same-file target by its assignment-target variable name.
	src := `from agents import Agent

multiply_agent = Agent(name="Multiply Agent", instructions="x2")
start_agent = Agent(
    name="Start Agent",
    instructions="hand off if odd",
    handoffs=[multiply_agent],
)
`
	pf := parsePyFile(t, "main.py", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})

	var start *models.AgentDef
	for i := range inv.Agents {
		if inv.Agents[i].Name == "Start Agent" {
			start = &inv.Agents[i]
		}
	}
	if start == nil {
		t.Fatalf("start agent not found; got %+v", inv.Agents)
	}
	if len(start.HandoffRefs) != 1 {
		t.Fatalf("start.HandoffRefs: got %d, want 1", len(start.HandoffRefs))
	}
	ref := start.HandoffRefs[0]
	if ref.External || ref.Resolved == nil {
		t.Fatalf("handoffs=[multiply_agent] should resolve to same-file target, got %+v", ref)
	}
	if ref.Resolved.Name != "Multiply Agent" {
		t.Errorf("resolved to wrong agent: %+v", ref.Resolved)
	}
}

func TestResolveEdges_TSADKHostedToolAppendedToInventory(t *testing.T) {
	// TS ADK agent carries a HostedToolRef from discovery; ResolveEdges
	// should append a HostedToolDef{SDK: SDKGoogleADK} to inv.HostedTools
	// (not SDKOpenAIAgents — verifies the SDK is set from whichever set
	// matched, not hardcoded to OpenAI).
	inv := &models.RepoInventory{
		Agents: []models.AgentDef{{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguageTypeScript,
			Location: models.Location{FilePath: "src/a.ts", Line: 10},
			HostedToolRefs: []models.HostedToolRef{
				{Class: "GoogleSearchTool", DefIndex: -1},
			},
		}},
	}
	analysis.ResolveEdges(inv, nil)
	if len(inv.HostedTools) != 1 {
		t.Fatalf("expected 1 HostedToolDef in inventory, got %d", len(inv.HostedTools))
	}
	got := inv.HostedTools[0]
	if got.Class != "GoogleSearchTool" {
		t.Errorf("wrong class: %q", got.Class)
	}
	if got.SDK != models.SDKGoogleADK {
		t.Errorf("wrong SDK: %q, want %q (ADK class must stamp ADK SDK, not OpenAI)",
			got.SDK, models.SDKGoogleADK)
	}
	if inv.Agents[0].HostedToolRefs[0].Resolved == nil {
		t.Errorf("HostedToolRef should be resolved after edges")
	}
}
