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
