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

// TestDiscoverTSMCPServers_CreateSdkMcpServerEndLine verifies that a
// multi-line createSdkMcpServer({...}) call populates EndLine > Line.
//
// Source line map (1-based):
//
//	1: (blank)
//	2: import { createSdkMcpServer } from "@anthropic-ai/claude-agent-sdk";
//	3: const srv = createSdkMcpServer({
//	4:   name: "my-tools",
//	5:   version: "1.0.0",
//	6: });
func TestDiscoverTSMCPServers_CreateSdkMcpServerEndLine(t *testing.T) {
	src := `
import { createSdkMcpServer } from "@anthropic-ai/claude-agent-sdk";
const srv = createSdkMcpServer({
  name: "my-tools",
  version: "1.0.0",
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(got), got)
	}
	m := got[0]
	if m.EndLine <= m.Line {
		t.Errorf("createSdkMcpServer: EndLine = %d, want > Line = %d", m.EndLine, m.Line)
	}
}

// TestDiscoverTSMCPServers_McpServersObjectEndLine verifies that an object
// literal inside mcpServers: {...} populates EndLine > Line when it spans
// multiple lines.
//
// Source line map:
//
//	1: (blank)
//	2: import { query } from "@anthropic-ai/claude-agent-sdk";
//	3: const q = query({
//	4:   options: {
//	5:     mcpServers: {
//	6:       a: {
//	7:         type: "stdio",
//	8:         command: "python",
//	9:         args: ["-m", "x"]
//	10:       }
//	11:     }
//	12:   }
//	13: });
func TestDiscoverTSMCPServers_McpServersObjectEndLine(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";
const q = query({
  options: {
    mcpServers: {
      a: {
        type: "stdio",
        command: "python",
        args: ["-m", "x"]
      }
    }
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(got), got)
	}
	m := got[0]
	if m.EndLine <= m.Line {
		t.Errorf("mcpServers object literal: EndLine = %d, want > Line = %d", m.EndLine, m.Line)
	}
}
