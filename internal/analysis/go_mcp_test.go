package analysis_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parseGoForTest(t *testing.T, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.NewGoParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: "main.go", Tree: tree, Source: []byte(src)}
}

func TestDiscoverGoMCPTools_Mark3labs(t *testing.T) {
	src := `package main

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func run(s *server.MCPServer) {
	calc := mcp.NewTool("calculate",
		mcp.WithDescription("Perform arithmetic"),
		mcp.WithString("operation", mcp.Required()),
		mcp.WithNumber("x", mcp.Required()),
	)
	s.AddTool(calc, nil)

	bare := mcp.NewTool("process")
	s.AddTool(bare, nil)
}
`
	tools := analysis.DiscoverGoMCPTools([]analysis.ParsedFile{parseGoForTest(t, src)}, nil)
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(tools), tools)
	}
	byName := map[string]models.ToolDef{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	calc, ok := byName["calculate"]
	if !ok {
		t.Fatalf("calculate not discovered: %+v", tools)
	}
	if calc.Description != "Perform arithmetic" {
		t.Errorf("description = %q, want %q", calc.Description, "Perform arithmetic")
	}
	if calc.Kind != models.KindMCPTool {
		t.Errorf("kind = %q, want mcp_tool", calc.Kind)
	}
	if calc.Language != models.LanguageGo {
		t.Errorf("language = %q, want go", calc.Language)
	}
	if !calc.HasTypedParams {
		t.Error("want HasTypedParams=true for WithString/WithNumber params")
	}
	if len(calc.ParamNames) != 2 {
		t.Errorf("ParamNames = %v, want [operation x]", calc.ParamNames)
	}
	bare, ok := byName["process"]
	if !ok {
		t.Fatal("process not discovered")
	}
	if bare.Description != "" {
		t.Errorf("bare tool description = %q, want empty (would fire MCP-015)", bare.Description)
	}
}

func TestDiscoverGoMCPTools_OfficialSDK(t *testing.T) {
	src := `package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

func run() {
	server := mcp.NewServer(&mcp.Implementation{Name: "greeter"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "say hi"}, nil)
}
`
	tools := analysis.DiscoverGoMCPTools([]analysis.ParsedFile{parseGoForTest(t, src)}, nil)
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d: %+v", len(tools), tools)
	}
	if tools[0].Name != "greet" {
		t.Errorf("name = %q, want greet", tools[0].Name)
	}
	if tools[0].Description != "say hi" {
		t.Errorf("description = %q, want %q", tools[0].Description, "say hi")
	}
	if tools[0].Language != models.LanguageGo {
		t.Errorf("language = %q, want go", tools[0].Language)
	}
	if tools[0].Kind != models.KindMCPTool {
		t.Errorf("kind = %q, want mcp_tool", tools[0].Kind)
	}
}

func TestDiscoverGoMCPTools_GateExcludesNonMCP(t *testing.T) {
	// A file that does not import an mcp-go module yields nothing, even if it
	// calls something named NewTool on a package coincidentally named mcp.
	src := `package main

import "example.com/other/mcp"

func run() {
	_ = mcp.NewTool("x", mcp.WithDescription("d"))
}
`
	tools := analysis.DiscoverGoMCPTools([]analysis.ParsedFile{parseGoForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("non-mcp-go import must gate out; got %d: %+v", len(tools), tools)
	}
}
