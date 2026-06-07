package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversPHPMCP is the end-to-end contract for PHP MCP support: a
// PHP project using an MCP SDK is classified by recon, parsed by tree-sitter-php,
// its #[McpTool] methods discovered (Kind=mcp_tool, Language=php) which stamps
// SDKMCP, and audited by the language:php rules in the shared mcp/ pack — the
// "process" tool with no description fires MCP-019 (no description) and MCP-020
// (ambiguous name).
func TestScanRun_DiscoversPHPMCP(t *testing.T) {
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
	mustWrite("composer.json", `{"require": {"php-mcp/server": "^2.0"}}`)
	mustWrite("Tools.php", `<?php
use PhpMcp\Server\Attributes\McpTool;

class Tools
{
    #[McpTool]
    public function process(string $input): string
    {
        return $input;
    }
}
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var phpTool bool
	for _, tl := range res.Tools {
		if tl.FilePath == "Tools.php" {
			phpTool = true
			if tl.Language != models.LanguagePHP {
				t.Errorf("tool %q from Tools.php: Language = %q, want php", tl.Name, tl.Language)
			}
			if tl.Kind != models.KindMCPTool {
				t.Errorf("tool %q from Tools.php: Kind = %q, want mcp_tool", tl.Name, tl.Kind)
			}
		}
	}
	if !phpTool {
		t.Fatalf("no tool discovered from Tools.php; Tools=%+v", res.Tools)
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

	want := map[string]bool{"MCP-019": false, "MCP-020": false}
	for _, f := range res.Findings {
		if f.FilePath == "Tools.php" {
			if _, ok := want[f.RuleID]; ok {
				want[f.RuleID] = true
			}
		}
	}
	for id, fired := range want {
		if !fired {
			t.Errorf("expected %s to fire on the PHP MCP tool in Tools.php; Findings=%+v", id, res.Findings)
		}
	}
}
