package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversJavaScript is the end-to-end contract for JavaScript
// support: plain .js (ES modules) flows through the TypeScript-family pipeline —
// parsed by the tsx grammar, discovered by the OpenAI Agents JS passes, re-tagged
// LanguageJavaScript for honest output, and audited by the existing
// `language: typescript` rule packs via the TS/JS family gate. The eval() handler
// must trigger OAI-017 (has_code_exec_call) attributed to the .js file.
func TestScanRun_DiscoversJavaScript(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("package.json", `{"dependencies": {"@openai/agents": "^0.1.0"}}`)
	mustWrite("app.js", `
import { Agent, tool } from "@openai/agents";
import { z } from "zod";

export const runCode = tool({
  name: "run_code",
  description: "Runs an expression",
  parameters: z.object({ expr: z.string() }),
  execute: async ({ expr }) => eval(expr),
});

export const agent = new Agent({ name: "a", instructions: "i", tools: [runCode] });
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A tool and an agent are discovered from the .js file, both honestly tagged
	// LanguageJavaScript (discovery stamps the TS family; retagJavaScriptDefs
	// corrects them after edge resolution).
	var jsTool, jsAgent bool
	for _, tl := range res.Tools {
		if tl.FilePath == "app.js" {
			jsTool = true
			if tl.Language != models.LanguageJavaScript {
				t.Errorf("tool %q from app.js: Language = %q, want javascript", tl.Name, tl.Language)
			}
		}
	}
	for _, a := range res.Agents {
		if a.FilePath == "app.js" {
			jsAgent = true
			if a.Language != models.LanguageJavaScript {
				t.Errorf("agent from app.js: Language = %q, want javascript", a.Language)
			}
		}
	}
	if !jsTool {
		t.Errorf("no tool discovered from app.js; Tools=%+v", res.Tools)
	}
	if !jsAgent {
		t.Errorf("no agent discovered from app.js; Agents=%+v", res.Agents)
	}

	// SDK recognized from the JS file's dependency + ES import.
	var sawOpenAI bool
	for _, s := range res.SDKs {
		if s == models.SDKOpenAIAgents {
			sawOpenAI = true
		}
	}
	if !sawOpenAI {
		t.Errorf("SDKs missing openai_agents: %+v", res.SDKs)
	}

	// The existing TypeScript rule packs audit the JS tool via the family gate:
	// the eval() handler must produce a finding attributed to app.js (OAI-017).
	var sawOAI017 bool
	for _, f := range res.Findings {
		if f.FilePath == "app.js" && f.RuleID == "OAI-017" {
			sawOAI017 = true
		}
	}
	if !sawOAI017 {
		t.Errorf("expected OAI-017 to fire on the JS eval() tool in app.js; Findings=%+v", res.Findings)
	}
}
