package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A file importing BOTH pydantic_ai and an MCP server SDK must keep @server.tool
// / @mcp.tool routed to MCP — the Pydantic `.tool` suffix routing must not
// swallow the MCP-reserved decorator shapes (which have no other discovery path
// in Python).
func TestKindFromDecorators_PydanticDoesNotStealMCP(t *testing.T) {
	src := `from pydantic_ai import Agent
from mcp.server.fastmcp import FastMCP

server = FastMCP("svc")

@server.tool()
def srv_tool(x: int) -> int:
    return x
`
	pf := parsePyFile(t, "both.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	kind := map[string]models.ToolKind{}
	for _, td := range tools {
		kind[td.Name] = td.Kind
	}
	if kind["srv_tool"] != models.KindMCPTool {
		t.Errorf("srv_tool: got kind %q, want %q (pydantic must not steal @server.tool)", kind["srv_tool"], models.KindMCPTool)
	}
}

// @agent.tool_plain is Pydantic-only (the Claude SDK has @agent.tool but no
// tool_plain), so it must route to Pydantic even when the file also imports the
// Claude SDK — the !claudeImport guard applies only to the colliding `.tool`.
func TestKindFromDecorators_PydanticToolPlainWithClaudeImport(t *testing.T) {
	src := `from pydantic_ai import Agent
from claude_agent_sdk import tool

agent = Agent("openai:gpt-4o")

@agent.tool_plain
def calc(x: int) -> int:
    return x
`
	pf := parsePyFile(t, "tp.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	kind := map[string]models.ToolKind{}
	for _, td := range tools {
		kind[td.Name] = td.Kind
	}
	if kind["calc"] != models.KindPydanticAITool {
		t.Errorf("calc: got kind %q, want %q (tool_plain has no Claude collision)", kind["calc"], models.KindPydanticAITool)
	}
}

// TestKindFromDecorators_PreciseCalleeMatching guards against the substring-scan
// false positives: matching "@tool" as a substring classified unrelated user
// decorators (@tool_registry.register, @toolbar) as Claude-SDK tools, firing
// tool rules on code that is not a tool. Classification now matches the
// decorator's resolved callee path.
func TestKindFromDecorators_PreciseCalleeMatching(t *testing.T) {
	src := `from agents import function_tool

@function_tool
def oai():
    pass

@tool
def claude():
    pass

@server.tool
def mcp_srv():
    pass

@app.register_tool
def mcp_reg():
    pass

@tool_registry.register
def not_a_tool():
    pass

@toolbar
def also_not():
    pass
`
	pf := parsePyFile(t, "main.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})

	kind := map[string]models.ToolKind{}
	for _, td := range tools {
		kind[td.Name] = td.Kind
	}

	want := map[string]models.ToolKind{
		"oai":     models.KindOpenAITool,
		"claude":  models.KindClaudeSDKTool,
		"mcp_srv": models.KindMCPTool,
		"mcp_reg": models.KindMCPTool,
	}
	for name, wantKind := range want {
		if kind[name] != wantKind {
			t.Errorf("%s: got kind %q, want %q", name, kind[name], wantKind)
		}
	}

	// Unrelated user decorators must not be classified as tools at all — an
	// Unknown kind is skipped in discovery, so these names must be absent.
	for _, name := range []string{"not_a_tool", "also_not"} {
		if k, ok := kind[name]; ok {
			t.Errorf("%s must not be classified as a tool, got kind %q", name, k)
		}
	}
}
