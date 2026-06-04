package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSMCPProper_RegisterTool(t *testing.T) {
	src := `
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const server = new McpServer({ name: "demo", version: "1.0.0" });

server.registerTool(
  "search_docs",
  { title: "Search", description: "Search the docs", inputSchema: { query: z.string() } },
  async ({ query }) => ({ content: [{ type: "text", text: query }] })
);
`
	pf := parseTSForTest(t, "src/server.ts", src)
	tools := analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1: %+v", len(tools), tools)
	}
	tool := tools[0]
	if tool.Name != "search_docs" {
		t.Errorf("Name = %q, want %q", tool.Name, "search_docs")
	}
	if tool.Description != "Search the docs" {
		t.Errorf("Description = %q", tool.Description)
	}
	if tool.Kind != models.KindMCPTool {
		t.Errorf("Kind = %q, want %q", tool.Kind, models.KindMCPTool)
	}
	if tool.Language != models.LanguageTypeScript {
		t.Errorf("Language = %q", tool.Language)
	}
	if !tool.HasTypedParams {
		t.Errorf("HasTypedParams should be true for a Zod inputSchema")
	}
	if len(tool.ParamNames) != 1 || tool.ParamNames[0] != "query" {
		t.Errorf("ParamNames = %v, want [query]", tool.ParamNames)
	}
}

func TestDiscoverTSMCPProper_LegacyToolForm(t *testing.T) {
	// server.tool(name, paramsSchema, handler) — the older overload.
	src := `
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
const server = new McpServer({ name: "demo", version: "1.0.0" });
server.tool("add", { a: z.number(), b: z.number() }, async ({ a, b }) => ({ content: [] }));
`
	pf := parseTSForTest(t, "src/server.ts", src)
	tools := analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1: %+v", len(tools), tools)
	}
	if tools[0].Name != "add" {
		t.Errorf("Name = %q, want add", tools[0].Name)
	}
	if !tools[0].HasTypedParams || len(tools[0].ParamNames) != 2 {
		t.Errorf("expected 2 typed params, got typed=%v names=%v", tools[0].HasTypedParams, tools[0].ParamNames)
	}
}

func TestDiscoverTSMCPProper_HandlerShellFact(t *testing.T) {
	src := `
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
const server = new McpServer({ name: "demo", version: "1.0.0" });
server.registerTool("run", { description: "run", inputSchema: { cmd: z.string() } }, async ({ cmd }) => {
  const { execSync } = require("child_process");
  execSync(cmd);
  return { content: [] };
});
`
	pf := parseTSForTest(t, "src/server.ts", src)
	tools := analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if tools[0].Facts["shells_out"] != "true" {
		t.Errorf("expected shells_out fact, got Facts=%v", tools[0].Facts)
	}
}

func TestDiscoverTSMCPProper_ReceiverAware(t *testing.T) {
	// A .tool()/.registerTool() call on something that is NOT a discovered
	// McpServer variable must not be mis-attributed as an MCP tool.
	src := `
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "demo", version: "1.0.0" });
const other = makeSomethingElse();
other.registerTool("not_mcp", { description: "x", inputSchema: {} }, async () => ({}));
other.tool("also_not_mcp", {}, async () => ({}));
`
	pf := parseTSForTest(t, "src/server.ts", src)
	tools := analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Fatalf("got %d tools, want 0 (calls were on a non-server receiver): %+v", len(tools), tools)
	}
}

func TestDiscoverTSMCPProper_ImportGate(t *testing.T) {
	// No @modelcontextprotocol/sdk import → nothing discovered, even though the
	// code shape matches.
	src := `
const server = new McpServer({ name: "demo", version: "1.0.0" });
server.registerTool("x", { description: "x", inputSchema: {} }, async () => ({}));
`
	pf := parseTSForTest(t, "src/server.ts", src)
	tools := analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Fatalf("got %d tools, want 0 (import gate)", len(tools))
	}
}
