package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSOpenAIMCPServers_AllThreeTransports(t *testing.T) {
	src := `
import { MCPServerStdio, MCPServerSSE, MCPServerStreamableHttp } from "@openai/agents";

const stdio = new MCPServerStdio({ command: "node", args: ["./s.js"] });
const sse   = new MCPServerSSE({ url: "https://x" });
const http  = new MCPServerStreamableHttp({ url: "https://y" });
`
	pf := parseTSForTest(t, "src/mcp.ts", src)
	got := analysis.DiscoverTSOpenAIMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 3 {
		t.Fatalf("got %d servers, want 3: %+v", len(got), got)
	}
	byClass := map[string]models.MCPServerDef{}
	for _, s := range got {
		byClass[s.Class] = s
	}
	if s := byClass["MCPServerStdio"]; s.Transport != "stdio" || s.VarName != "stdio" {
		t.Errorf("MCPServerStdio: %+v", s)
	}
	if s := byClass["MCPServerSSE"]; s.Transport != "sse" || s.VarName != "sse" {
		t.Errorf("MCPServerSSE: %+v", s)
	}
	if s := byClass["MCPServerStreamableHttp"]; s.Transport != "streamable_http" || s.VarName != "http" {
		t.Errorf("MCPServerStreamableHttp: %+v", s)
	}
	for _, s := range got {
		if s.SDK != models.SDKOpenAIAgents {
			t.Errorf("SDK = %q, want %q", s.SDK, models.SDKOpenAIAgents)
		}
		if s.Language != models.LanguageTypeScript {
			t.Errorf("Language = %q", s.Language)
		}
	}
}

func TestDiscoverTSOpenAIMCPServers_MultiWrapper(t *testing.T) {
	src := `
import { MCPServers, MCPServerStdio } from "@openai/agents";
const stdio = new MCPServerStdio({ command: "x" });
const multi = new MCPServers([stdio]);
`
	pf := parseTSForTest(t, "src/m.ts", src)
	got := analysis.DiscoverTSOpenAIMCPServers([]analysis.ParsedFile{pf}, nil)
	var multi *models.MCPServerDef
	for i, s := range got {
		if s.Class == "MCPServers" {
			multi = &got[i]
		}
	}
	if multi == nil {
		t.Fatal("MCPServers wrapper not discovered")
	}
	if multi.Transport != "multi" {
		t.Errorf("MCPServers Transport = %q, want %q", multi.Transport, "multi")
	}
	if multi.VarName != "multi" {
		t.Errorf("MCPServers VarName = %q, want %q", multi.VarName, "multi")
	}
}

func TestDiscoverTSOpenAIMCPServers_NoImportGate(t *testing.T) {
	src := `
class MCPServerStdio { constructor(opts) {} }  // user-defined, no SDK import
const x = new MCPServerStdio({});
`
	pf := parseTSForTest(t, "src/m.ts", src)
	got := analysis.DiscoverTSOpenAIMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 0 {
		t.Errorf("no-SDK-import should produce zero servers, got %+v", got)
	}
}

func TestDiscoverTSOpenAIMCPServers_RenamedImport(t *testing.T) {
	src := `
import { MCPServerStdio as Stdio } from "@openai/agents-core";
const s = new Stdio({ command: "x" });
`
	pf := parseTSForTest(t, "src/m.ts", src)
	got := analysis.DiscoverTSOpenAIMCPServers([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 || got[0].Class != "MCPServerStdio" {
		t.Errorf("renamed import: got %+v", got)
	}
}
