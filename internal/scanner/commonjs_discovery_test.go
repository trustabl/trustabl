package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_DiscoversCommonJS is the end-to-end contract for CommonJS support:
// a .cjs OpenAI Agents app that imports via `const { tool } = require(...)` (no
// ES import) is classified by recon, parsed by the tsx grammar, gated in via the
// require()-aware import collector, discovered, re-tagged LanguageJavaScript, and
// audited by the existing language:typescript rule packs — OAI-017 must fire on
// its eval() handler.
func TestScanRun_DiscoversCommonJS(t *testing.T) {
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
	mustWrite("app.cjs", `
const { Agent, tool } = require("@openai/agents");
const { z } = require("zod");

const runCode = tool({
  name: "run_code",
  description: "Runs an expression",
  parameters: z.object({ expr: z.string() }),
  execute: async ({ expr }) => eval(expr),
});

const agent = new Agent({ name: "a", instructions: "i", tools: [runCode] });
module.exports = { runCode, agent };
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var jsTool bool
	for _, tl := range res.Tools {
		if tl.FilePath == "app.cjs" {
			jsTool = true
			if tl.Language != models.LanguageJavaScript {
				t.Errorf("tool from app.cjs: Language = %q, want javascript", tl.Language)
			}
		}
	}
	if !jsTool {
		t.Errorf("no tool discovered from app.cjs — the CommonJS require() gate failed; Tools=%+v", res.Tools)
	}

	var sawOAI017 bool
	for _, f := range res.Findings {
		if f.FilePath == "app.cjs" && f.RuleID == "OAI-017" {
			sawOAI017 = true
		}
	}
	if !sawOAI017 {
		t.Errorf("expected OAI-017 to fire on the CommonJS eval() tool in app.cjs; Findings=%+v", res.Findings)
	}
}
