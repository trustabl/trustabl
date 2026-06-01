package analysis_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parseTSForTest(t *testing.T, path, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: path, Tree: tree, Source: []byte(src)}
}

func TestDiscoverTSTools_BasicToolFactory(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";

const searchTool = tool(
  "search",
  "Search the web",
  { query: z.string() },
  async ({ query }) => { return { content: [] }; }
);
`
	pf := parseTSForTest(t, "src/agent.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1: %+v", len(tools), tools)
	}
	tool := tools[0]
	if tool.Name != "search" {
		t.Errorf("Name: got %q, want %q", tool.Name, "search")
	}
	if tool.Description != "Search the web" {
		t.Errorf("Description: got %q", tool.Description)
	}
	if tool.Kind != models.KindClaudeSDKTool {
		t.Errorf("Kind: got %q, want %q", tool.Kind, models.KindClaudeSDKTool)
	}
	if tool.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q, want %q", tool.Language, models.LanguageTypeScript)
	}
	if !tool.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true (Zod schemas always type)")
	}
	if len(tool.ParamNames) != 1 || tool.ParamNames[0] != "query" {
		t.Errorf("ParamNames: got %+v, want [query]", tool.ParamNames)
	}
	// Location must span the full multi-line tool() call. EndLine was left at
	// the zero value before, violating the Location invariant (every entity
	// sets EndLine — single-line entities set EndLine == Line, never 0).
	if tool.Line != 5 {
		t.Errorf("Line: got %d, want 5", tool.Line)
	}
	if tool.EndLine != 10 {
		t.Errorf("EndLine: got %d, want 10 (full tool() call span, not 0)", tool.EndLine)
	}
}

func TestDiscoverTSTools_NoImportGate_NoExtraction(t *testing.T) {
	src := `
const tool = (name) => name;  // local function named "tool", no SDK import
const searchTool = tool("search", "desc", {}, async () => {});
`
	pf := parseTSForTest(t, "src/agent.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Errorf("expected zero tools (no SDK import), got %d: %+v", len(tools), tools)
	}
}

func TestDiscoverTSTools_RenamedImport(t *testing.T) {
	src := `
import { tool as t } from "@anthropic-ai/claude-agent-sdk";
const x = t("search", "Search", {}, async () => {});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Errorf("renamed import: got %+v", tools)
	}
}

func TestDiscoverTSTools_NamespaceImport(t *testing.T) {
	src := `
import * as sdk from "@anthropic-ai/claude-agent-sdk";
const x = sdk.tool("search", "Search", {}, async () => {});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Errorf("namespace import: got %+v", tools)
	}
}

func TestDiscoverTSTools_HandlerFacts(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";

const shellTool = tool("run", "Run", {}, async () => {
  const { execSync } = require("child_process");
  execSync("ls");
});

const httpTool = tool("fetch", "Fetch", {}, async () => {
  await fetch("https://example.com");
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2: %+v", len(tools), tools)
	}
	var shell, http models.ToolDef
	for _, x := range tools {
		switch x.Name {
		case "run":
			shell = x
		case "fetch":
			http = x
		}
	}
	if shell.Facts["shells_out"] != "true" {
		t.Errorf("shells_out: got %q, want true", shell.Facts["shells_out"])
	}
	if http.Facts["http_call"] != "true" {
		t.Errorf("http_call: got %q, want true", http.Facts["http_call"])
	}
}

func TestDiscoverTSTools_ExtrasConfigCapture(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
const x = tool("search", "Search", {}, async () => {},
  { annotations: { readOnlyHint: true } });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d", len(tools))
	}
	if tools[0].Config["annotations.readOnlyHint"] != "true" {
		t.Errorf("Config[annotations.readOnlyHint]: got %q, want \"true\"", tools[0].Config["annotations.readOnlyHint"])
	}
}

func TestDiscoverTSTools_NonLiteralName_EmptyName(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
const n = getName();
const x = tool(n, "Search", {}, async () => {});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 || tools[0].Name != "" {
		t.Errorf("expected one tool with empty Name (non-literal arg 0), got %+v", tools)
	}
}
