package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSADKTools_BasicConstructor(t *testing.T) {
	src := `
import { FunctionTool } from "@google/adk";

const computeSum = new FunctionTool({
  name: "sum",
  description: "Add two numbers",
  parameters: { a: 0, b: 0 },
  execute: async ({ a, b }) => String(a + b),
});
`
	pf := parseTSForTest(t, "src/tools.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
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
	if tool.Kind != models.KindADKFunctionTool {
		t.Errorf("Kind = %q, want %q", tool.Kind, models.KindADKFunctionTool)
	}
	if tool.Language != models.LanguageTypeScript {
		t.Errorf("Language = %q", tool.Language)
	}
	if !tool.HasTypedParams {
		t.Errorf("HasTypedParams should be true for non-empty parameters object")
	}
	if len(tool.ParamNames) != 2 || tool.ParamNames[0] != "a" || tool.ParamNames[1] != "b" {
		t.Errorf("ParamNames = %v, want [a b]", tool.ParamNames)
	}
}

func TestDiscoverTSADKTools_ZodParameters(t *testing.T) {
	// Regression (TR-147): a Zod schema constructor as parameters must count as
	// typed and enumerate its keys, same as the inline object-literal form.
	src := `
import { FunctionTool } from "@google/adk";
import { z } from "zod";
const t = new FunctionTool({
  name: "geo",
  description: "d",
  parameters: z.object({ lat: z.number(), lon: z.number() }),
  execute: async () => "",
});
`
	pf := parseTSForTest(t, "src/tools.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	got := tools[0]
	if !got.HasTypedParams {
		t.Errorf("HasTypedParams = false; Zod schema must count as typed")
	}
	if len(got.ParamNames) != 2 || got.ParamNames[0] != "lat" || got.ParamNames[1] != "lon" {
		t.Errorf("ParamNames = %v, want [lat lon]", got.ParamNames)
	}
}

func TestDiscoverTSADKTools_NoImportGate(t *testing.T) {
	src := `
class FunctionTool { constructor(opts) {} }
const t = new FunctionTool({ name: "fake", description: "no SDK import" });
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Errorf("expected zero (no SDK import), got %d: %+v", len(tools), tools)
	}
}

func TestDiscoverTSADKTools_RenamedImport(t *testing.T) {
	src := `
import { FunctionTool as FT } from "@google/adk";
const x = new FT({ name: "renamed", description: "d", parameters: {}, execute: async () => "" });
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 || tools[0].Name != "renamed" {
		t.Errorf("renamed import: got %+v", tools)
	}
}

func TestDiscoverTSADKTools_NonObjectArg_SkippedSilently(t *testing.T) {
	src := `
import { FunctionTool } from "@google/adk";
const x = new FunctionTool(someComputedOptions);
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 0 {
		t.Errorf("non-object arg should be skipped silently, got %+v", tools)
	}
}

func TestDiscoverTSADKTools_HandlerFacts(t *testing.T) {
	src := `
import { FunctionTool } from "@google/adk";

const httpTool = new FunctionTool({
  name: "fetcher",
  description: "fetch",
  parameters: {},
  execute: async () => { await fetch("https://example.com"); return ""; },
});
const shellTool = new FunctionTool({
  name: "runner",
  description: "run",
  parameters: {},
  execute: async () => { const { execSync } = require("child_process"); execSync("ls"); return ""; },
});
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	var http, shell models.ToolDef
	for _, x := range tools {
		switch x.Name {
		case "fetcher":
			http = x
		case "runner":
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

func TestDiscoverTSADKTools_ConfigFlattens(t *testing.T) {
	src := `
import { FunctionTool } from "@google/adk";
const x = new FunctionTool({
  name: "x",
  description: "d",
  parameters: {},
  execute: async () => "",
  isLongRunning: true,
});
`
	pf := parseTSForTest(t, "src/x.ts", src)
	tools := analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d", len(tools))
	}
	cfg := tools[0].Config
	if cfg["isLongRunning"] != "true" {
		t.Errorf("Config flatten missing isLongRunning, got: %+v", cfg)
	}
}
