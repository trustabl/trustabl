package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// kindByName runs tool discovery over one parsed file and returns a name→Kind map.
func kindByName(t *testing.T, path, src string) map[string]models.ToolKind {
	t.Helper()
	pf := parsePyFile(t, path, src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	out := map[string]models.ToolKind{}
	for _, td := range tools {
		out[td.Name] = td.Kind
	}
	return out
}

// REGRESSION GUARD (the load-bearing half of the kindFromDecorators rework):
// the Claude Agent SDK also exposes an `@agent.tool` decorator. Routing
// `*.tool` / `*.tool_plain` attribute decorators to Pydantic must NOT change
// Claude behavior — a file that imports the Claude SDK and uses `@agent.tool`
// must still classify as KindClaudeSDKTool via the existing switch case. If this
// breaks, the `&& !claudeImport` guard in kindFromDecorators is wrong.
func TestKindFromDecorators_ClaudeAgentToolUnchanged(t *testing.T) {
	src := `from claude_agent_sdk import ClaudeSDKClient

agent = ClaudeSDKClient()

@agent.tool
def lookup(q: str) -> str:
    """Look something up."""
    return q
`
	kind := kindByName(t, "claude_tools.py", src)
	if kind["lookup"] != models.KindClaudeSDKTool {
		t.Errorf("@agent.tool in a Claude-SDK file: got kind %q, want %q (the Pydantic rework must not steal Claude's @agent.tool)",
			kind["lookup"], models.KindClaudeSDKTool)
	}
}

// A `@agent.tool` / `@agent.tool_plain` attribute decorator in a file that
// imports pydantic_ai (and NOT the Claude SDK) routes to KindPydanticAITool.
func TestKindFromDecorators_PydanticAgentTool(t *testing.T) {
	src := `from pydantic_ai import Agent, RunContext

agent = Agent("openai:gpt-4o")

@agent.tool
def ctx_tool(ctx: RunContext[None], q: str) -> str:
    """A context tool."""
    return q

@agent.tool_plain
def plain_tool(q: str) -> str:
    """A plain tool."""
    return q
`
	kind := kindByName(t, "pyd_tools.py", src)
	if kind["ctx_tool"] != models.KindPydanticAITool {
		t.Errorf("@agent.tool in a pydantic_ai file: got %q, want %q", kind["ctx_tool"], models.KindPydanticAITool)
	}
	if kind["plain_tool"] != models.KindPydanticAITool {
		t.Errorf("@agent.tool_plain in a pydantic_ai file: got %q, want %q", kind["plain_tool"], models.KindPydanticAITool)
	}
}

// Collision precedence: a file importing BOTH pydantic_ai and the Claude SDK
// must keep `@agent.tool` as KindClaudeSDKTool (claude wins, mirroring the
// import-binding precedence for the bare `@tool` collision).
func TestKindFromDecorators_BothImportsClaudeWins(t *testing.T) {
	src := `from pydantic_ai import Agent
from claude_agent_sdk import ClaudeSDKClient

agent = Agent("openai:gpt-4o")

@agent.tool
def shared(q: str) -> str:
    """Shared shape."""
    return q
`
	kind := kindByName(t, "both.py", src)
	if kind["shared"] != models.KindClaudeSDKTool {
		t.Errorf("@agent.tool in a file importing BOTH pydantic_ai and Claude: got %q, want %q (claude must win)",
			kind["shared"], models.KindClaudeSDKTool)
	}
}

// The non-decorator `Tool(fn, ...)` factory in a pydantic_ai file is discovered
// as a KindPydanticAITool ToolDef, resolving the first positional ident to a
// same-file function for docstring + typed-param recovery; an explicit name=
// overrides the wrapped function's name.
func TestPydanticAITool_FactoryDiscovered(t *testing.T) {
	src := `from pydantic_ai import Agent, Tool

def fetch(url: str) -> str:
    """Fetch a URL."""
    import requests
    return requests.get(url).text

my_tool = Tool(fetch, takes_ctx=False, name="fetcher")
`
	pf := parsePyFile(t, "factory.py", src)
	tools := analysis.DiscoverPydanticAITools([]analysis.ParsedFile{pf})
	if len(tools) != 1 {
		t.Fatalf("got %d Pydantic tools, want 1: %+v", len(tools), tools)
	}
	tl := tools[0]
	if tl.Kind != models.KindPydanticAITool {
		t.Errorf("Kind: got %q, want %q", tl.Kind, models.KindPydanticAITool)
	}
	if tl.Name != "fetcher" {
		t.Errorf("Name: got %q, want fetcher (explicit name= override)", tl.Name)
	}
	if tl.Description != "Fetch a URL." {
		t.Errorf("Description: got %q, want the wrapped function docstring", tl.Description)
	}
	if tl.VarName != "my_tool" {
		t.Errorf("VarName: got %q, want my_tool", tl.VarName)
	}
}

// The Tool(...) factory is import-gated: a Tool(...) call in a file that imports
// neither pydantic_ai nor LangChain yields no Pydantic tool, and a file that
// imports LangChain (which owns the colliding Tool name) is also skipped.
func TestPydanticAITool_FactoryGate(t *testing.T) {
	// No pydantic_ai import → nothing.
	plain := `def Tool(fn, **kw):
    return fn

def fetch(url: str) -> str:
    """Fetch."""
    return url

t = Tool(fetch)
`
	pf := parsePyFile(t, "plain.py", plain)
	if tools := analysis.DiscoverPydanticAITools([]analysis.ParsedFile{pf}); len(tools) != 0 {
		t.Errorf("unimported Tool() must not be a Pydantic tool; got %+v", tools)
	}
	// Imports LangChain (owns Tool) → Pydantic discovery defers.
	lc := `from langchain_core.tools import Tool
from pydantic_ai import Agent

def fetch(url: str) -> str:
    """Fetch."""
    return url

t = Tool(fetch)
`
	pf2 := parsePyFile(t, "lc.py", lc)
	if tools := analysis.DiscoverPydanticAITools([]analysis.ParsedFile{pf2}); len(tools) != 0 {
		t.Errorf("Tool() in a LangChain-importing file must defer to LangChain; got %+v", tools)
	}
}
