package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversCSharpMCP is the end-to-end contract for C# MCP support:
// a .NET project using the official ModelContextProtocol SDK is classified by
// recon, parsed by tree-sitter-c-sharp, its [McpServerTool] methods discovered
// (Kind=mcp_tool, Language=csharp) which stamps SDKMCP, and audited by the
// language:csharp rules in the shared mcp/ pack — the "Process" tool with no
// description fires both MCP-017 (no description) and MCP-018 (ambiguous name).
func TestScanRun_DiscoversCSharpMCP(t *testing.T) {
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
	mustWrite("Directory.Packages.props", "<Project>\n  <ItemGroup>\n    <PackageVersion Include=\"ModelContextProtocol\" Version=\"0.3.0\" />\n  </ItemGroup>\n</Project>\n")
	mustWrite("Tools.cs", `using ModelContextProtocol.Server;

[McpServerToolType]
public class Tools
{
    [McpServerTool]
    public static string Process(string input)
    {
        return input;
    }
}
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var csTool bool
	for _, tl := range res.Tools {
		if tl.FilePath == "Tools.cs" {
			csTool = true
			if tl.Language != models.LanguageCSharp {
				t.Errorf("tool %q from Tools.cs: Language = %q, want csharp", tl.Name, tl.Language)
			}
			if tl.Kind != models.KindMCPTool {
				t.Errorf("tool %q from Tools.cs: Kind = %q, want mcp_tool", tl.Name, tl.Kind)
			}
		}
	}
	if !csTool {
		t.Fatalf("no tool discovered from Tools.cs; Tools=%+v", res.Tools)
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

	want := map[string]bool{"MCP-017": false, "MCP-018": false}
	for _, f := range res.Findings {
		if f.FilePath == "Tools.cs" {
			if _, ok := want[f.RuleID]; ok {
				want[f.RuleID] = true
			}
		}
	}
	for id, fired := range want {
		if !fired {
			t.Errorf("expected %s to fire on the C# MCP tool in Tools.cs; Findings=%+v", id, res.Findings)
		}
	}
}
