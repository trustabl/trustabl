package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSOpenAITools_BasicFactory(t *testing.T) {
	src := `
import { tool } from "@openai/agents";
import { z } from "zod";

const computeSum = tool({
  name: "sum",
  description: "Add two numbers",
  parameters: z.object({ a: z.number(), b: z.number() }),
  execute: async ({ a, b }) => String(a + b),
});
`
	pf := parseTSForTest(t, "src/tools.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1: %+v", len(tools), tools)
	}
	tool := tools[0]
	if tool.Name != "sum" {
		t.Errorf("Name = %q, want %q", tool.Name, "sum")
	}
	if tool.VarName != "computeSum" {
		t.Errorf("VarName = %q, want %q", tool.VarName, "computeSum")
	}
	if tool.Description != "Add two numbers" {
		t.Errorf("Description = %q", tool.Description)
	}
	if tool.Kind != models.KindOpenAITool {
		t.Errorf("Kind = %q, want %q", tool.Kind, models.KindOpenAITool)
	}
	if tool.Language != models.LanguageTypeScript {
		t.Errorf("Language = %q", tool.Language)
	}
}

func TestDiscoverTSOpenAITools_NoImportGate(t *testing.T) {
	src := `
const tool = (opts) => opts;
const x = tool({ name: "fake", description: "no SDK import" });
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Errorf("expected zero (no SDK import), got %d", len(tools))
	}
}

func TestDiscoverTSOpenAITools_FromAgentsCore(t *testing.T) {
	src := `
import { tool } from "@openai/agents-core";
const t = tool({ name: "x", description: "y", parameters: {}, execute: async () => "" });
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("import from @openai/agents-core should match union gate, got %d", len(tools))
	}
}

func TestDiscoverTSOpenAITools_RenamedImport(t *testing.T) {
	src := `
import { tool as makeT } from "@openai/agents";
const x = makeT({ name: "renamed", description: "d", parameters: {}, execute: async () => "" });
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 || tools[0].Name != "renamed" {
		t.Errorf("renamed import: got %+v", tools)
	}
}

func TestDiscoverTSOpenAITools_NonObjectArg_SkippedSilently(t *testing.T) {
	src := `
import { tool } from "@openai/agents";
const x = tool(someComputedOptions);
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Errorf("non-object arg should be skipped silently, got %+v", tools)
	}
}

func TestDiscoverTSOpenAITools_HandlerFacts(t *testing.T) {
	src := `
import { tool } from "@openai/agents";
const httpTool = tool({
  name: "fetcher",
  description: "fetch",
  parameters: {},
  execute: async () => { await fetch("https://example.com"); return ""; },
});
const shellTool = tool({
  name: "runner",
  description: "run",
  parameters: {},
  execute: async () => { const { execSync } = require("child_process"); execSync("ls"); return ""; },
});
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	var http, shell models.ToolDef
	for _, x := range tools {
		if x.Name == "fetcher" {
			http = x
		}
		if x.Name == "runner" {
			shell = x
		}
	}
	if http.Facts["http_call"] != "true" {
		t.Errorf("fetcher should record http_call=true, got %v", http.Facts)
	}
	if shell.Facts["shells_out"] != "true" {
		t.Errorf("runner should record shells_out=true, got %v", shell.Facts)
	}
}

// TestTSHandlerFacts_NamespacedChildProcess covers the namespace-import shape
// `child_process.exec(...)` (from `import * as child_process` /
// `const child_process = require("child_process")`), not just the destructured
// bare `exec(...)` form. The callee text is `child_process.exec`, which the
// bare-identifier match misses.
func TestTSHandlerFacts_NamespacedChildProcess(t *testing.T) {
	const src = `
import { tool } from "@openai/agents";
const runner = tool({
  name: "runner",
  description: "run",
  parameters: {},
  execute: async () => { child_process.exec("ls"); return ""; },
});
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if tools[0].Facts["shells_out"] != "true" {
		t.Errorf("namespaced child_process.exec should record shells_out=true, got %v", tools[0].Facts)
	}
}

func TestDiscoverTSOpenAITools_ConfigFlattens(t *testing.T) {
	src := `
import { tool } from "@openai/agents";
const x = tool({
  name: "x",
  description: "d",
  parameters: {},
  execute: async () => "",
  strict: true,
  needsApproval: false,
  timeoutMs: 5000,
});
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d", len(tools))
	}
	cfg := tools[0].Config
	if cfg["strict"] != "true" || cfg["needsApproval"] != "false" || cfg["timeoutMs"] != "5000" {
		t.Errorf("Config flatten incomplete: %+v", cfg)
	}
}
