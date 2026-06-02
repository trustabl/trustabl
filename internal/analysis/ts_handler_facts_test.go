package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
)

func tsToolFacts(t *testing.T, src string) map[string]string {
	t.Helper()
	pf := parseTSForTest(t, "src/a.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, func(string) {})
	if len(tools) == 0 {
		t.Fatal("no tool discovered")
	}
	return tools[0].Facts
}

func TestTSHandlerFacts_DynamicURL_InterpolatedHits(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", { host: z.string() }, async ({ host }) => {
  const r = await fetch(` + "`https://${host}/api`" + `);
  return { content: [] };
});
`
	if tsToolFacts(t, src)["dynamic_url"] != "true" {
		t.Error("expected dynamic_url=true for interpolated fetch URL")
	}
}

func TestTSHandlerFacts_DynamicURL_LiteralIsSilent(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", {}, async () => {
  const r = await fetch("https://example.com/api");
  return { content: [] };
});
`
	if tsToolFacts(t, src)["dynamic_url"] == "true" {
		t.Error("expected no dynamic_url for a literal fetch URL")
	}
}

func TestTSHandlerFacts_CodeExec_EvalHits(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", { e: z.string() }, async ({ e }) => {
  return { content: [{ type: "text", text: String(eval(e)) }] };
});
`
	if tsToolFacts(t, src)["code_exec"] != "true" {
		t.Error("expected code_exec=true for eval() call")
	}
}

func TestTSHandlerFacts_CodeExec_NewFunctionHits(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", { b: z.string() }, async ({ b }) => {
  const fn = new Function("return " + b);
  return { content: [] };
});
`
	if tsToolFacts(t, src)["code_exec"] != "true" {
		t.Error("expected code_exec=true for new Function(...)")
	}
}

func TestTSHandlerFacts_CodeExec_RetrievalIsSilent(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", { q: z.string() }, async ({ q }) => {
  const r = await retrieval(q);
  return { content: [{ type: "text", text: r }] };
});
`
	if tsToolFacts(t, src)["code_exec"] == "true" {
		t.Error("retrieval( must NOT set code_exec (the false-positive this fix targets)")
	}
}
