package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversGoMCP is the end-to-end contract for Go MCP support: a Go
// repo using mark3labs/mcp-go is classified by recon, parsed by tree-sitter-go,
// its mcp.NewTool(...) tools discovered (Kind=mcp_tool, Language=go) which stamps
// SDKMCP, and audited by the language:go rules in the shared mcp/ pack — the
// "process" tool with no description fires both MCP-015 (no description) and
// MCP-016 (ambiguous name).
func TestScanRun_DiscoversGoMCP(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module example.com/srv\n\ngo 1.22\n\nrequire github.com/mark3labs/mcp-go v0.30.0\n")
	mustWrite("main.go", `package main

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("demo", "1.0.0")
	tool := mcp.NewTool("process", mcp.WithString("input", mcp.Required()))
	s.AddTool(tool, nil)
}
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var goTool bool
	for _, tl := range res.Tools {
		if tl.FilePath == "main.go" {
			goTool = true
			if tl.Language != models.LanguageGo {
				t.Errorf("tool %q from main.go: Language = %q, want go", tl.Name, tl.Language)
			}
			if tl.Kind != models.KindMCPTool {
				t.Errorf("tool %q from main.go: Kind = %q, want mcp_tool", tl.Name, tl.Kind)
			}
		}
	}
	if !goTool {
		t.Fatalf("no tool discovered from main.go; Tools=%+v", res.Tools)
	}

	var sawMCP bool
	for _, s := range res.SDKs {
		if s == models.SDKMCP {
			sawMCP = true
		}
	}
	if !sawMCP {
		t.Errorf("SDKs missing mcp: %+v", res.SDKs)
	}

	want := map[string]bool{"MCP-015": false, "MCP-016": false}
	for _, f := range res.Findings {
		if f.FilePath == "main.go" {
			if _, ok := want[f.RuleID]; ok {
				want[f.RuleID] = true
			}
		}
	}
	for id, fired := range want {
		if !fired {
			t.Errorf("expected %s to fire on the Go MCP tool in main.go; Findings=%+v", id, res.Findings)
		}
	}
}
