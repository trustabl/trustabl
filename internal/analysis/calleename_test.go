package analysis

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestCalleeName_StripsModuleQualifier(t *testing.T) {
	cases := map[string]string{
		"WebSearchTool()":        "WebSearchTool",
		"agents.WebSearchTool()": "WebSearchTool",
		"a.b.WebSearchTool(x=1)": "WebSearchTool",
		"MCPServerStdio()":       "MCPServerStdio",
		"NoParens":               "NoParens",
		"mod.NoParens":           "NoParens",
	}
	for in, want := range cases {
		if got := calleeName(in); got != want {
			t.Errorf("calleeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestClassify_QualifiedCallsAreRecognized is the regression for TR-153:
// module-qualified hosted-tool, ADK hosted-tool, and MCP-server calls must
// classify, not fall through to an external ref.
func TestClassify_QualifiedCallsAreRecognized(t *testing.T) {
	ht := models.Expr{Kind: models.ExprCall, Text: "agents.WebSearchTool()", Line: 3, EndLine: 3}
	if h, ok := classifyHostedToolCall(ht, "main.py"); !ok || h.Class != "WebSearchTool" {
		t.Errorf("qualified hosted tool: ok=%v class=%q, want true/WebSearchTool", ok, h.Class)
	}
	adk := models.Expr{Kind: models.ExprCall, Text: "tools.BashTool()", Line: 4, EndLine: 4}
	if h, ok := classifyADKHostedToolCall(adk, "main.py"); !ok || h.Class != "BashTool" {
		t.Errorf("qualified ADK hosted tool: ok=%v class=%q, want true/BashTool", ok, h.Class)
	}
	mcp := models.Expr{Kind: models.ExprCall, Text: "mcp.MCPServerStdio()", Line: 5, EndLine: 5}
	if m, ok := classifyMCPServerCall(mcp, "main.py"); !ok || m.Class != "MCPServerStdio" {
		t.Errorf("qualified MCP server: ok=%v class=%q, want true/MCPServerStdio", ok, m.Class)
	}
}
