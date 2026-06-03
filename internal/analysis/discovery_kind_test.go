package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

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
