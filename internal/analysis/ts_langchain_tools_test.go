package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestTSLangChain_ToolFactory(t *testing.T) {
	src := `import { tool } from "@langchain/core/tools";
import { z } from "zod";

const getWeather = tool(
  async (input) => { return "sunny in " + input.city; },
  { name: "get_weather", description: "Get the weather.", schema: z.object({ city: z.string() }) }
);
`
	pf := parseTSForTest(t, "tools.ts", src)
	tools := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := lcFindTool(tools, "get_weather")
	if !ok {
		t.Fatalf("tool 'get_weather' not discovered; got %+v", tools)
	}
	if tl.Kind != models.KindLangChainTool {
		t.Errorf("Kind: got %q, want %q", tl.Kind, models.KindLangChainTool)
	}
	if tl.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q, want typescript", tl.Language)
	}
	if tl.Description != "Get the weather." {
		t.Errorf("Description: got %q", tl.Description)
	}
	if !tl.HasTypedParams {
		t.Errorf("HasTypedParams: got false, want true (zod schema)")
	}
	if tl.VarName != "getWeather" {
		t.Errorf("VarName: got %q, want getWeather", tl.VarName)
	}
}

func TestTSLangChain_DynamicStructuredTool(t *testing.T) {
	src := `import { DynamicStructuredTool } from "@langchain/core/tools";
import { z } from "zod";
const look = new DynamicStructuredTool({ name: "lookup", description: "Look up.", schema: z.object({ q: z.string() }), func: async (i) => i.q });
`
	pf := parseTSForTest(t, "dst.ts", src)
	tools := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	if _, ok := lcFindTool(tools, "lookup"); !ok {
		t.Fatalf("DynamicStructuredTool not discovered; got %+v", tools)
	}
}

func TestTSLangChain_DynamicTool(t *testing.T) {
	src := `import { DynamicTool } from "@langchain/core/tools";
const echo = new DynamicTool({ name: "echo", description: "Echo.", func: async (i) => i });
`
	pf := parseTSForTest(t, "dt.ts", src)
	tools := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := lcFindTool(tools, "echo")
	if !ok {
		t.Fatalf("DynamicTool not discovered; got %+v", tools)
	}
	if tl.HasTypedParams {
		t.Errorf("DynamicTool has no schema; HasTypedParams should be false")
	}
}

// The strongest collision proof: a LangChain tool() must be found by the
// LangChain pass and NOT by the OpenAI pass (and vice versa), purely via the
// import gate.
func TestTSLangChain_NoCrossFireWithOpenAI(t *testing.T) {
	src := `import { tool } from "@langchain/core/tools";
import { z } from "zod";
const t = tool(async (i) => "", { name: "lc_tool", description: "x", schema: z.object({}) });
`
	pf := parseTSForTest(t, "lc.ts", src)
	lc := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	oai := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(lc) != 1 {
		t.Errorf("LangChain pass: got %d tools, want 1", len(lc))
	}
	if len(oai) != 0 {
		t.Errorf("OpenAI pass must not discover a LangChain tool; got %d (%+v)", len(oai), oai)
	}
}

func TestTSLangChain_ToolShellFact(t *testing.T) {
	src := `import { tool } from "@langchain/core/tools";
import { execSync } from "child_process";
import { z } from "zod";
const run = tool(
  async (i) => { return execSync(i.cmd).toString(); },
  { name: "run_cmd", description: "Run.", schema: z.object({ cmd: z.string() }) }
);
`
	pf := parseTSForTest(t, "shell.ts", src)
	tools := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := lcFindTool(tools, "run_cmd")
	if !ok {
		t.Fatalf("tool 'run_cmd' not discovered")
	}
	if tl.Facts["shells_out"] != "true" {
		t.Errorf("shells_out: got %q, want \"true\"; facts=%v", tl.Facts["shells_out"], tl.Facts)
	}
}

func TestTSLangChain_ToolSSRFFact(t *testing.T) {
	src := `import { tool } from "@langchain/core/tools";
import { z } from "zod";
const fetchUrl = tool(
  async (i) => { const r = await fetch(i.url); return r.text(); },
  { name: "fetch_url", description: "Fetch.", schema: z.object({ url: z.string() }) }
);
`
	pf := parseTSForTest(t, "ssrf.ts", src)
	tools := analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, nil)
	tl, ok := lcFindTool(tools, "fetch_url")
	if !ok {
		t.Fatalf("tool 'fetch_url' not discovered")
	}
	if tl.Facts["dynamic_url"] != "true" {
		t.Errorf("dynamic_url: got %q, want \"true\"; facts=%v", tl.Facts["dynamic_url"], tl.Facts)
	}
}
