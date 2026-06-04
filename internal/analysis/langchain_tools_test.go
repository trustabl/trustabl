package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func lcFindTool(tools []models.ToolDef, name string) (models.ToolDef, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return models.ToolDef{}, false
}

// The headline collision test: bare @tool is shared by LangChain and the Claude
// SDK. A langchain-importing file must route it to LangChain.
func TestLangChain_ToolDecorator_RoutesToLangChain(t *testing.T) {
	src := `from langchain_core.tools import tool

@tool
def search(query: str) -> str:
    """Search the web."""
    return do_search(query)
`
	pf := parsePyFile(t, "tools.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	tl, ok := lcFindTool(tools, "search")
	if !ok {
		t.Fatalf("tool 'search' not discovered; got %+v", tools)
	}
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("Kind: got %q, want %q", tl.Kind, models.KindLangChainTool)
	}
	if tl.Language != models.LanguagePython {
		t.Errorf("Language: got %q, want python", tl.Language)
	}
}

// A Claude-SDK @tool must NOT be reclassified — the historical default holds
// when langchain is not imported.
func TestLangChain_ToolDecorator_ClaudeStaysClaude(t *testing.T) {
	src := `from claude_agent_sdk import tool

@tool
def read_file(path: str) -> str:
    """Read a file."""
    return open(path).read()
`
	pf := parsePyFile(t, "claude_tools.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	tl, ok := lcFindTool(tools, "read_file")
	if !ok {
		t.Fatalf("tool 'read_file' not discovered")
	}
	if tl.Kind != models.KindClaudeSDKTool {
		t.Errorf("Kind: got %q, want %q (Claude must not be reclassified)", tl.Kind, models.KindClaudeSDKTool)
	}
}

// @tool is resolved by the import binding of the `tool` symbol, not by
// file-level import presence — so it attributes correctly no matter what else
// the file imports. The cases below pin both directions and the shadowing rule.

// LangChain provides `tool` while the Claude SDK is imported for something else
// → LangChain. (A file-level "claude is imported, so Claude wins" heuristic gets
// this wrong; binding resolution fixes it.)
func TestLangChain_ToolBinding_LangChainToolWithClaudeImport(t *testing.T) {
	src := `from claude_agent_sdk import AgentDefinition
from langchain_core.tools import tool

@tool
def amb(q: str) -> str:
    """Ambiguous name, langchain binding."""
    return q
`
	pf := parsePyFile(t, "mixed_lc.py", src)
	tl, ok := lcFindTool(analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}), "amb")
	if !ok {
		t.Fatalf("tool 'amb' not discovered")
	}
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("got %q, want langchain_tool (tool bound to langchain)", tl.Kind)
	}
}

// Claude provides `tool` while langchain is imported for something else → Claude.
func TestLangChain_ToolBinding_ClaudeToolWithLangChainImport(t *testing.T) {
	src := `from langchain_core.tools import StructuredTool
from claude_agent_sdk import tool

@tool
def amb(q: str) -> str:
    """Ambiguous name, claude binding."""
    return q
`
	pf := parsePyFile(t, "mixed_claude.py", src)
	tl, ok := lcFindTool(analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}), "amb")
	if !ok {
		t.Fatalf("tool 'amb' not discovered")
	}
	if tl.Kind != models.KindClaudeSDKTool {
		t.Errorf("got %q, want claude_sdk_tool (tool bound to claude)", tl.Kind)
	}
}

// Shadowing: both import bare `tool`; the last import binds (Python semantics).
func TestLangChain_ToolBinding_ShadowLastWins(t *testing.T) {
	lcLast := `from claude_agent_sdk import tool
from langchain_core.tools import tool

@tool
def amb(q: str) -> str:
    """x."""
    return q
`
	pf := parsePyFile(t, "shadow_lc.py", lcLast)
	tl, _ := lcFindTool(analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}), "amb")
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("langchain-imported-last: got %q, want langchain_tool", tl.Kind)
	}

	claudeLast := `from langchain_core.tools import tool
from claude_agent_sdk import tool

@tool
def amb(q: str) -> str:
    """x."""
    return q
`
	pf2 := parsePyFile(t, "shadow_claude.py", claudeLast)
	tl2, _ := lcFindTool(analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf2}), "amb")
	if tl2.Kind != models.KindClaudeSDKTool {
		t.Errorf("claude-imported-last: got %q, want claude_sdk_tool", tl2.Kind)
	}
}

// An aliased import resolves by the local alias name.
func TestLangChain_ToolBinding_Aliased(t *testing.T) {
	src := `from claude_agent_sdk import tool
from langchain_core.tools import tool as lc_tool

@lc_tool
def amb(q: str) -> str:
    """x."""
    return q
`
	pf := parsePyFile(t, "aliased.py", src)
	tl, ok := lcFindTool(analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf}), "amb")
	if !ok {
		t.Fatalf("tool 'amb' not discovered")
	}
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("aliased @lc_tool: got %q, want langchain_tool", tl.Kind)
	}
}

func TestLangChain_StructuredToolFromFunction(t *testing.T) {
	src := `from langchain_core.tools import StructuredTool

def add(a: int, b: int) -> int:
    """Add two numbers."""
    return a + b

calc = StructuredTool.from_function(add)
`
	pf := parsePyFile(t, "calc.py", src)
	tools := analysis.DiscoverLangChainTools([]analysis.ParsedFile{pf})
	tl, ok := lcFindTool(tools, "add")
	if !ok {
		t.Fatalf("StructuredTool.from_function not discovered; got %+v", tools)
	}
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("Kind: got %q, want %q", tl.Kind, models.KindLangChainTool)
	}
	if tl.Description != "Add two numbers." {
		t.Errorf("Description: got %q, want %q", tl.Description, "Add two numbers.")
	}
	if !tl.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true (typed signature)")
	}
	if tl.VarName != "calc" {
		t.Errorf("VarName: got %q, want %q", tl.VarName, "calc")
	}
}

func TestLangChain_ConstructorsExplicitName(t *testing.T) {
	src := `from langchain.tools import StructuredTool, Tool

def _impl(x):
    return x

t1 = StructuredTool(name="lookup", description="Look it up.", func=_impl)
t2 = Tool(name="legacy", description="Legacy tool.", func=_impl)
`
	pf := parsePyFile(t, "ctors.py", src)
	tools := analysis.DiscoverLangChainTools([]analysis.ParsedFile{pf})
	if _, ok := lcFindTool(tools, "lookup"); !ok {
		t.Errorf("StructuredTool(name=lookup) not discovered; got %+v", tools)
	}
	if _, ok := lcFindTool(tools, "legacy"); !ok {
		t.Errorf("Tool(name=legacy) not discovered; got %+v", tools)
	}
}

// The import gate must keep a user-defined Tool(...) in a non-langchain file
// from being swept up.
func TestLangChain_GateExcludesNonLangChain(t *testing.T) {
	src := `class Tool:
    def __init__(self, **kw): pass

x = Tool(name="not_a_langchain_tool")
`
	pf := parsePyFile(t, "unrelated.py", src)
	tools := analysis.DiscoverLangChainTools([]analysis.ParsedFile{pf})
	if len(tools) != 0 {
		t.Errorf("non-langchain Tool() should not be discovered; got %+v", tools)
	}
}

func TestLangChain_StructuredToolShellFact(t *testing.T) {
	src := `import subprocess
from langchain_core.tools import StructuredTool

def run_cmd(cmd: str) -> str:
    """Run a command."""
    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()

danger = StructuredTool.from_function(run_cmd)
`
	pf := parsePyFile(t, "shell.py", src)
	tools := analysis.DiscoverLangChainTools([]analysis.ParsedFile{pf})
	tl, ok := lcFindTool(tools, "run_cmd")
	if !ok {
		t.Fatalf("tool 'run_cmd' not discovered")
	}
	if tl.Facts["shells_out"] != "true" {
		t.Errorf("shells_out fact: got %q, want \"true\"", tl.Facts["shells_out"])
	}
}
