package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// vaiFindTool finds a discovered Vercel tool by its VarName (Vercel tools carry
// no Name — the model-facing name is the agent's tools-record key).
func vaiFindTool(tools []models.ToolDef, varName string) (models.ToolDef, bool) {
	for _, td := range tools {
		if td.VarName == varName {
			return td, true
		}
	}
	return models.ToolDef{}, false
}

func TestTSVercel_ToolFactoryTyped(t *testing.T) {
	src := `import { tool } from "ai";
import { z } from "zod";
const weatherTool = tool({
  description: "Get the weather.",
  inputSchema: z.object({ city: z.string() }),
  execute: async ({ city }) => "sunny in " + city,
});
`
	pf := parseTSForTest(t, "tools.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := vaiFindTool(tools, "weatherTool")
	if !ok {
		t.Fatalf("tool 'weatherTool' not discovered; got %+v", tools)
	}
	if tl.Kind != models.KindVercelAITool {
		t.Errorf("Kind: got %q, want %q", tl.Kind, models.KindVercelAITool)
	}
	if tl.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q, want typescript", tl.Language)
	}
	if tl.Description != "Get the weather." {
		t.Errorf("Description: got %q", tl.Description)
	}
	if !tl.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true (inputSchema zod object)")
	}
	if tl.Name != "" {
		t.Errorf("Name: got %q, want empty (Vercel derives name from the tools-record key)", tl.Name)
	}
}

// v4 used `parameters`; v5/v6 use `inputSchema`. Both must yield typed params.
func TestTSVercel_ToolParametersAlias(t *testing.T) {
	src := `import { tool } from "ai";
import { z } from "zod";
const t = tool({ description: "x", parameters: z.object({ q: z.string() }), execute: async ({ q }) => q });
`
	pf := parseTSForTest(t, "v4.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := vaiFindTool(tools, "t")
	if !ok {
		t.Fatalf("v4 parameters tool not discovered; got %+v", tools)
	}
	if !tl.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true (v4 parameters zod object)")
	}
}

func TestTSVercel_DynamicToolIsUntyped(t *testing.T) {
	src := `import { dynamicTool } from "ai";
import { z } from "zod";
const echo = dynamicTool({ description: "Echo.", inputSchema: z.unknown(), execute: async (input) => input });
`
	pf := parseTSForTest(t, "dyn.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := vaiFindTool(tools, "echo")
	if !ok {
		t.Fatalf("dynamicTool not discovered; got %+v", tools)
	}
	if tl.HasTypedParams {
		t.Errorf("dynamicTool input is always unknown; HasTypedParams should be false")
	}
	if len(tl.ParamNames) == 0 {
		t.Errorf("dynamicTool should still register a synthetic param so has_params holds")
	}
}

func TestTSVercel_EmptyObjectSchemaIsUntyped(t *testing.T) {
	src := `import { tool } from "ai";
import { z } from "zod";
const open = tool({ description: "x", inputSchema: z.object({}), execute: async () => 1 });
`
	pf := parseTSForTest(t, "open.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := vaiFindTool(tools, "open")
	if !ok {
		t.Fatalf("open-schema tool not discovered; got %+v", tools)
	}
	if tl.HasTypedParams {
		t.Errorf("z.object({}) imposes no field types; HasTypedParams should be false")
	}
	if len(tl.ParamNames) == 0 {
		t.Errorf("an open schema present means the tool takes input; has_params should hold")
	}
}

func TestTSVercel_NoDescription(t *testing.T) {
	src := `import { tool } from "ai";
import { z } from "zod";
const t = tool({ inputSchema: z.object({ q: z.string() }), execute: async ({ q }) => q });
`
	pf := parseTSForTest(t, "nodesc.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := vaiFindTool(tools, "t")
	if !ok {
		t.Fatalf("tool not discovered; got %+v", tools)
	}
	if tl.Description != "" {
		t.Errorf("Description: got %q, want empty", tl.Description)
	}
}

func TestTSVercel_ToolShellAndSSRFFacts(t *testing.T) {
	src := `import { tool } from "ai";
import { z } from "zod";
import { execSync } from "child_process";
const run = tool({ description: "run", inputSchema: z.object({ cmd: z.string() }), execute: async ({ cmd }) => execSync(cmd).toString() });
const fetchUrl = tool({ description: "get", inputSchema: z.object({ url: z.string() }), execute: async ({ url }) => (await fetch(url)).text() });
`
	pf := parseTSForTest(t, "facts.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	run, ok := vaiFindTool(tools, "run")
	if !ok {
		t.Fatalf("tool 'run' not discovered")
	}
	if run.Facts["shells_out"] != "true" {
		t.Errorf("shells_out: got %q, want true; facts=%v", run.Facts["shells_out"], run.Facts)
	}
	fu, ok := vaiFindTool(tools, "fetchUrl")
	if !ok {
		t.Fatalf("tool 'fetchUrl' not discovered")
	}
	if fu.Facts["dynamic_url"] != "true" {
		t.Errorf("dynamic_url: got %q, want true; facts=%v", fu.Facts["dynamic_url"], fu.Facts)
	}
}

// Collision guard: a tool({...}) in a file importing @langchain/core/tools must
// be discovered by the LangChain pass, NOT the Vercel pass — the `ai`-module
// import gate is the disambiguator.
func TestTSVercel_NoCrossFireWithLangChain(t *testing.T) {
	src := `import { tool } from "@langchain/core/tools";
import { z } from "zod";
const lc = tool(async (i) => "", { name: "lc_tool", description: "x", schema: z.object({ q: z.string() }) });
`
	pf := parseTSForTest(t, "lc.ts", src)
	vai := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	if len(vai) != 0 {
		t.Errorf("Vercel pass must not discover a LangChain tool; got %d (%+v)", len(vai), vai)
	}
	lc := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	if len(lc) != 1 {
		t.Errorf("LangChain pass: got %d tools, want 1", len(lc))
	}
}
