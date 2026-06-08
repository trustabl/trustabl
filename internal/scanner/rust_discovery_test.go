package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversRustMCP is the end-to-end contract for Rust MCP support: a
// Rust project using the rmcp crate is classified by recon, parsed by
// tree-sitter-rust, its #[tool] methods discovered (Kind=mcp_tool, Language=rust)
// which stamps SDKMCP, and audited by the language:rust rules in the shared mcp/
// pack — the "process" tool with no description fires MCP-021 (no description)
// and MCP-022 (ambiguous name).
func TestScanRun_DiscoversRustMCP(t *testing.T) {
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
	mustWrite("Cargo.toml", "[package]\nname = \"srv\"\n\n[dependencies]\nrmcp = \"0.1\"\n")
	mustWrite("src/tools.rs", `use rmcp::{tool, tool_router};

#[tool_router]
impl Server {
    #[tool]
    fn process(&self) -> String {
        String::new()
    }
}
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var rustTool bool
	for _, tl := range res.Tools {
		if tl.FilePath == "src/tools.rs" {
			rustTool = true
			if tl.Language != models.LanguageRust {
				t.Errorf("tool %q from src/tools.rs: Language = %q, want rust", tl.Name, tl.Language)
			}
			if tl.Kind != models.KindMCPTool {
				t.Errorf("tool %q from src/tools.rs: Kind = %q, want mcp_tool", tl.Name, tl.Kind)
			}
		}
	}
	if !rustTool {
		t.Fatalf("no tool discovered from src/tools.rs; Tools=%+v", res.Tools)
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

	want := map[string]bool{"MCP-021": false, "MCP-022": false}
	for _, f := range res.Findings {
		if f.FilePath == "src/tools.rs" {
			if _, ok := want[f.RuleID]; ok {
				want[f.RuleID] = true
			}
		}
	}
	for id, fired := range want {
		if !fired {
			t.Errorf("expected %s to fire on the Rust MCP tool in src/tools.rs; Findings=%+v", id, res.Findings)
		}
	}
}
