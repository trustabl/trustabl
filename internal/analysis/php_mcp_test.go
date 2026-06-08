package analysis_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parsePHPForTest(t *testing.T, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.NewPHPParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: "Tools.php", Tree: tree, Source: []byte(src)}
}

func TestDiscoverPHPMCPTools(t *testing.T) {
	src := `<?php
use PhpMcp\Server\Attributes\McpTool;

class CalculatorTools {
    #[McpTool(name: 'add', description: 'Add two numbers')]
    public function add(int $a, int $b): int { return $a + $b; }

    #[McpTool]
    public function process(string $input): string { return $input; }
}
`
	tools := analysis.DiscoverPHPMCPTools([]analysis.ParsedFile{parsePHPForTest(t, src)}, nil)
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(tools), tools)
	}
	byName := map[string]models.ToolDef{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	add, ok := byName["add"]
	if !ok {
		t.Fatalf("add not discovered (name: arg not read?): %+v", tools)
	}
	if add.Description != "Add two numbers" {
		t.Errorf("description = %q", add.Description)
	}
	if add.Language != models.LanguagePHP {
		t.Errorf("language = %q, want php", add.Language)
	}
	if add.Kind != models.KindMCPTool {
		t.Errorf("kind = %q, want mcp_tool", add.Kind)
	}
	if !add.HasTypedParams {
		t.Error("want HasTypedParams=true (int $a, int $b)")
	}
	if len(add.ParamNames) != 2 {
		t.Errorf("ParamNames = %v, want [a b]", add.ParamNames)
	}
	proc, ok := byName["process"]
	if !ok {
		t.Fatalf("process not discovered (method-name fallback failed): %+v", tools)
	}
	if proc.Description != "" {
		t.Errorf("process description = %q, want empty (would fire MCP-019)", proc.Description)
	}
}

func TestDiscoverPHPMCPTools_GateExcludesNonMCP(t *testing.T) {
	src := `<?php
class Tools {
    #[McpTool(name: 'add')]
    public function add(int $a): int { return $a; }
}
`
	tools := analysis.DiscoverPHPMCPTools([]analysis.ParsedFile{parsePHPForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("file without a `use ...Mcp...` must gate out; got %d: %+v", len(tools), tools)
	}
}

// TestDiscoverPHPMCPTools_McpToolboxNotMatched guards against the substring
// match: #[McpToolbox] / #[McpToolFactory] are different attributes and must not
// be read as #[McpTool].
func TestDiscoverPHPMCPTools_McpToolboxNotMatched(t *testing.T) {
	src := `<?php
use PhpMcp\Server\Attributes\McpTool;

class Tools {
    #[McpToolbox(name: 'notatool')]
    public function box(): void {}

    #[McpTool(name: 'real')]
    public function real(int $a): int { return $a; }
}
`
	tools := analysis.DiscoverPHPMCPTools([]analysis.ParsedFile{parsePHPForTest(t, src)}, nil)
	if len(tools) != 1 {
		t.Fatalf("only #[McpTool] should match, not #[McpToolbox]; got %d: %+v", len(tools), tools)
	}
	if tools[0].Name != "real" {
		t.Errorf("name = %q, want real", tools[0].Name)
	}
}

// TestDiscoverPHPMCPTools_McpenzieNamespaceNotGate guards the file gate: an
// unrelated `use App\Mcpenzie\...` (which merely contains "Mcp") must not open
// the MCP gate.
func TestDiscoverPHPMCPTools_McpenzieNamespaceNotGate(t *testing.T) {
	src := `<?php
use App\Mcpenzie\Client;

class Tools {
    #[McpTool(name: 'add')]
    public function add(int $a): int { return $a; }
}
`
	tools := analysis.DiscoverPHPMCPTools([]analysis.ParsedFile{parsePHPForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("a non-MCP `use App\\Mcpenzie\\...` must not open the gate; got %d: %+v", len(tools), tools)
	}
}
