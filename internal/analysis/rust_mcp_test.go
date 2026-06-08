package analysis_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parseRustForTest(t *testing.T, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.NewRustParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: "tools.rs", Tree: tree, Source: []byte(src)}
}

func TestDiscoverRustMCPTools(t *testing.T) {
	src := `use rmcp::{tool, tool_router};

#[tool_router]
impl Calculator {
    /// Add two numbers together.
    #[tool(description = "Add two numbers")]
    fn add(&self, Parameters(p): Parameters<AddParams>) -> String { String::new() }

    #[tool(name = "do", description = "")]
    fn process(&self) -> String { String::new() }

    /// Fetch the current weather for a city.
    #[tool]
    fn fetch_weather(&self) -> String { String::new() }
}
`
	tools := analysis.DiscoverRustMCPTools([]analysis.ParsedFile{parseRustForTest(t, src)}, nil)
	if len(tools) != 3 {
		t.Fatalf("want 3 tools, got %d: %+v", len(tools), tools)
	}
	byName := map[string]models.ToolDef{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}

	add, ok := byName["add"]
	if !ok {
		t.Fatalf("add not discovered: %+v", tools)
	}
	if add.Description != "Add two numbers" {
		t.Errorf("add.Description = %q, want the attribute arg", add.Description)
	}
	if add.Language != models.LanguageRust {
		t.Errorf("add.Language = %q, want rust", add.Language)
	}
	if add.Kind != models.KindMCPTool {
		t.Errorf("add.Kind = %q, want mcp_tool", add.Kind)
	}
	if !add.HasTypedParams {
		t.Error("add: want HasTypedParams=true")
	}

	// The key Rust wrinkle: a bare #[tool] with a /// doc comment derives its
	// description from the doc comment, so MCP-021 must NOT fire on it.
	weather, ok := byName["fetch_weather"]
	if !ok {
		t.Fatalf("fetch_weather not discovered (method-name fallback failed): %+v", tools)
	}
	if weather.Description != "Fetch the current weather for a city." {
		t.Errorf("fetch_weather.Description = %q, want the /// doc comment", weather.Description)
	}

	// name = "do" override (ambiguous → MCP-022), description = "" (→ MCP-021).
	proc, ok := byName["do"]
	if !ok {
		t.Fatalf("process tool not discovered under its name override %q: %+v", "do", tools)
	}
	if proc.Description != "" {
		t.Errorf("process.Description = %q, want empty (empty arg, no doc → MCP-021)", proc.Description)
	}
}

func TestDiscoverRustMCPTools_GateExcludesNonMCP(t *testing.T) {
	src := `use std::collections::HashMap;

impl Calculator {
    #[tool(description = "Add")]
    fn add(&self) -> String { String::new() }
}
`
	tools := analysis.DiscoverRustMCPTools([]analysis.ParsedFile{parseRustForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("file without a `use rmcp` must gate out; got %d: %+v", len(tools), tools)
	}
}

// TestDiscoverRustMCPTools_WarmcpNotGate guards the file gate: a `use
// warmcp_utils::...` (which merely contains the substring "rmcp") is not the
// rmcp crate and must not open the gate.
func TestDiscoverRustMCPTools_WarmcpNotGate(t *testing.T) {
	src := `use warmcp_utils::Helper;

impl Calculator {
    #[tool(description = "Add")]
    fn add(&self) -> String { String::new() }
}
`
	tools := analysis.DiscoverRustMCPTools([]analysis.ParsedFile{parseRustForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("a `use warmcp_utils` (not the rmcp crate) must gate out; got %d: %+v", len(tools), tools)
	}
}
