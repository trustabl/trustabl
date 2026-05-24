package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSMCPServers_CreateSdkMcpServer(t *testing.T) {
	src := `
import { createSdkMcpServer } from "@anthropic-ai/claude-agent-sdk";
const srv = createSdkMcpServer({ name: "my-tools", version: "1.0.0" });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(got), got)
	}
	m := got[0]
	if m.Class != "createSdkMcpServer" {
		t.Errorf("Class: got %q want createSdkMcpServer", m.Class)
	}
	if m.Transport != "sdk" {
		t.Errorf("Transport: got %q want sdk", m.Transport)
	}
	if m.SDK != models.SDKClaudeAgentSDK || m.Language != models.LanguageTypeScript {
		t.Errorf("SDK/Language wrong: %+v", m)
	}
}

func TestDiscoverTSMCPServers_FourConfigTypes(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  options: {
    mcpServers: {
      a: { type: "stdio", command: "python", args: ["-m", "x"] },
      b: { type: "sse",   url: "https://a.b" },
      c: { type: "http",  url: "https://c.d" },
      d: { type: "sdk",   name: "srv" }
    }
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 4 {
		t.Fatalf("got %d, want 4: %+v", len(got), got)
	}
	wantTransports := map[string]string{
		"McpStdioServerConfig":           "stdio",
		"McpSSEServerConfig":             "sse",
		"McpHttpServerConfig":            "http",
		"McpSdkServerConfigWithInstance": "sdk",
	}
	seen := map[string]bool{}
	for _, m := range got {
		if wantTransports[m.Class] != m.Transport {
			t.Errorf("class %q: transport got %q want %q", m.Class, m.Transport, wantTransports[m.Class])
		}
		seen[m.Class] = true
	}
	if len(seen) != 4 {
		t.Errorf("not all four classes seen: %+v", seen)
	}
}
