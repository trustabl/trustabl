package rules_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/detectors"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// fixtureFS returns the rule packs from the repo-root testdata/rules-fixture
// directory — the Phase-1 interim home of the packs (they move to the
// trustabl-rules repo in Phase 2).
func fixtureFS(t *testing.T) fs.FS {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "rules-fixture")
	return os.DirFS(root)
}

// loadToolRule fetches a tool-scoped rule from shipped policies as a ToolDetector.
func loadToolRule(t *testing.T, ruleID string) detectors.ToolDetector {
	t.Helper()
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == ruleID && r.Scope == models.ScopeTool {
				return rules.NewToolRuleDetector(r)
			}
		}
	}
	t.Fatalf("tool-scoped rule %s not found in shipped policies", ruleID)
	return nil
}

// loadAgentRule fetches an agent-scoped rule from shipped policies as an AgentDetector.
func loadAgentRule(t *testing.T, ruleID string) detectors.AgentDetector {
	t.Helper()
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == ruleID && r.Scope == models.ScopeAgent {
				return rules.NewAgentRuleDetector(r)
			}
		}
	}
	t.Fatalf("agent-scoped rule %s not found in shipped policies", ruleID)
	return nil
}

// loadRepoRule fetches a repo-scoped rule from shipped policies as a RepoDetector.
func loadRepoRule(t *testing.T, ruleID string) detectors.RepoDetector {
	t.Helper()
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == ruleID && r.Scope == models.ScopeRepo {
				return rules.NewRepoRuleDetector(r)
			}
		}
	}
	t.Fatalf("repo-scoped rule %s not found in shipped policies", ruleID)
	return nil
}

// loadSubagentRule fetches a subagent-scoped rule as a SubagentDetector.
func loadSubagentRule(t *testing.T, ruleID string) detectors.SubagentDetector {
	t.Helper()
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == ruleID && r.Scope == models.ScopeSubagent {
				return rules.NewSubagentRuleDetector(r)
			}
		}
	}
	t.Fatalf("subagent-scoped rule %s not found in shipped policies", ruleID)
	return nil
}

// policyRuleCase is one fire-or-silent test against a shipped tool-scoped rule.
type policyRuleCase struct {
	name       string            // test subname
	ruleID     string            // YAML rule ID under test
	kind       models.ToolKind   // ToolKind for the synthetic tool
	src        string            // Python OR TypeScript snippet, per lang
	toolConfig map[string]string // optional Config override (for decorator-kwarg rules)
	wantFires  bool              // expected: rule fires for this snippet
	lang       models.Language   // "" defaults to python (existing cases)
}

// policyAgentCase is one fire-or-silent test against a shipped agent-scoped rule.
type policyAgentCase struct {
	name      string
	ruleID    string
	agent     models.AgentDef
	inv       models.RepoInventory
	wantFires bool
}

// policyRepoCase is one fire-or-silent test against a shipped repo-scoped rule.
type policyRepoCase struct {
	name      string
	ruleID    string
	profile   models.RepoProfile
	inv       models.RepoInventory
	wantFires bool
}

// policySubagentCase is one fire-or-silent test against a shipped subagent-scoped rule.
type policySubagentCase struct {
	name      string
	ruleID    string
	subagent  models.SubagentDef
	inv       models.RepoInventory
	wantFires bool
}

var policyRuleCases = []policyRuleCase{
	// ─── LangChain tool rules (LC-*) ────────────────────────────────────────
	{name: "LC-001 fires on tool with no docstring", ruleID: "LC-001", kind: models.KindLangChainTool, src: `
def search(q: str) -> str:
    return q
`, wantFires: true},
	{name: "LC-001 silent with docstring", ruleID: "LC-001", kind: models.KindLangChainTool, src: `
def search(q: str) -> str:
    """Search the web."""
    return q
`, wantFires: false},

	{name: "LC-002 fires on untyped params", ruleID: "LC-002", kind: models.KindLangChainTool, src: `
def search(q):
    """Search."""
    return q
`, wantFires: true},
	{name: "LC-002 silent with typed params", ruleID: "LC-002", kind: models.KindLangChainTool, src: `
def search(q: str) -> str:
    """Search."""
    return q
`, wantFires: false},

	{name: "LC-003 fires on subprocess", ruleID: "LC-003", kind: models.KindLangChainTool, src: `
def run(cmd: str) -> str:
    """Run a command."""
    import subprocess
    return subprocess.run(cmd, shell=True)
`, wantFires: true},
	{name: "LC-003 silent without shell", ruleID: "LC-003", kind: models.KindLangChainTool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd
`, wantFires: false},

	{name: "LC-004 fires on eval", ruleID: "LC-004", kind: models.KindLangChainTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return eval(expr)
`, wantFires: true},
	{name: "LC-004 silent without eval", ruleID: "LC-004", kind: models.KindLangChainTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return expr
`, wantFires: false},

	{name: "LC-005 fires on dynamic URL", ruleID: "LC-005", kind: models.KindLangChainTool, src: `
def fetch(url: str) -> str:
    """Fetch a URL."""
    import requests
    return requests.get(url).text
`, wantFires: true},
	{name: "LC-005 silent on literal URL", ruleID: "LC-005", kind: models.KindLangChainTool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: false},

	{name: "LC-006 fires on return_direct=True", ruleID: "LC-006", kind: models.KindLangChainTool, src: `
def f(x: str) -> str:
    """Doc."""
    return x
`, toolConfig: map[string]string{"return_direct": "True"}, wantFires: true},
	{name: "LC-006 silent without return_direct", ruleID: "LC-006", kind: models.KindLangChainTool, src: `
def f(x: str) -> str:
    """Doc."""
    return x
`, toolConfig: map[string]string{}, wantFires: false},

	// LangChain TypeScript tool rules — parseTSTool runs DiscoverTSLangChainTools,
	// so each snippet must import from the langchain ecosystem to be discovered.
	{name: "LC-010 fires on TS tool with no description", ruleID: "LC-010", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => i.q, { name: "search", schema: z.object({ q: z.string() }) });
`},
	{name: "LC-010 silent with description", ruleID: "LC-010", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => i.q, { name: "search", description: "Search the web.", schema: z.object({ q: z.string() }) });
`},

	{name: "LC-011 fires on TS subprocess", ruleID: "LC-011", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "@langchain/core/tools";
import { execSync } from "child_process";
import { z } from "zod";
export const t = tool(async (i) => { return execSync(i.cmd).toString(); }, { name: "run", description: "Run.", schema: z.object({ cmd: z.string() }) });
`},
	{name: "LC-011 silent without TS shell", ruleID: "LC-011", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => i.cmd, { name: "run", description: "Run.", schema: z.object({ cmd: z.string() }) });
`},

	{name: "LC-012 fires on TS eval", ruleID: "LC-012", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => { return eval(i.code); }, { name: "calc", description: "Calc.", schema: z.object({ code: z.string() }) });
`},
	{name: "LC-012 silent without TS eval", ruleID: "LC-012", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => { return i.code; }, { name: "calc", description: "Calc.", schema: z.object({ code: z.string() }) });
`},

	{name: "LC-013 fires on TS dynamic URL", ruleID: "LC-013", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => { const r = await fetch(i.url); return r.text(); }, { name: "fetch", description: "Fetch.", schema: z.object({ url: z.string() }) });
`},
	{name: "LC-013 silent on TS literal URL", ruleID: "LC-013", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => { const r = await fetch("https://api.example.com/data"); return r.text(); }, { name: "fetch", description: "Fetch.", schema: z.object({ q: z.string() }) });
`},

	{name: "LC-014 fires on TS returnDirect", ruleID: "LC-014", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => i.q, { name: "x", description: "X.", schema: z.object({ q: z.string() }), returnDirect: true });
`},
	{name: "LC-014 silent without TS returnDirect", ruleID: "LC-014", kind: models.KindLangChainTool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "@langchain/core/tools";
import { z } from "zod";
export const t = tool(async (i) => i.q, { name: "x", description: "X.", schema: z.object({ q: z.string() }) });
`},

	// ─── Vercel AI SDK tool rules (VAI-*) ───────────────────────────────────
	// Import-gated to the bare `ai` module; tool name comes from the agent's
	// tools-record KEY so ToolDef.Name is empty (VarName carries the binding).
	{name: "VAI-001 fires on TS subprocess", ruleID: "VAI-001", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "ai";
import { z } from "zod";
import { execSync } from "child_process";
export const t = tool({ description: "run", inputSchema: z.object({ cmd: z.string() }), execute: async ({ cmd }) => {
  return execSync(cmd).toString();
} });
`},
	{name: "VAI-001 silent without TS shell", ruleID: "VAI-001", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "add", inputSchema: z.object({ a: z.number() }), execute: async ({ a }) => a + 1 });
`},

	{name: "VAI-002 fires on TS eval", ruleID: "VAI-002", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "calc", inputSchema: z.object({ expr: z.string() }), execute: async ({ expr }) => eval(expr) });
`},
	{name: "VAI-002 silent without TS eval", ruleID: "VAI-002", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "calc", inputSchema: z.object({ a: z.number() }), execute: async ({ a }) => a * 2 });
`},

	{name: "VAI-003 fires on TS dynamic URL", ruleID: "VAI-003", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "get", inputSchema: z.object({ url: z.string() }), execute: async ({ url }) => {
  const r = await fetch(url);
  return r.text();
} });
`},
	{name: "VAI-003 silent on TS literal URL", ruleID: "VAI-003", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "get", inputSchema: z.object({ q: z.string() }), execute: async () => {
  const r = await fetch("https://api.example.com/status");
  return r.text();
} });
`},

	{name: "VAI-004 fires on tool with no description", ruleID: "VAI-004", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ inputSchema: z.object({ q: z.string() }), execute: async ({ q }) => q });
`},
	{name: "VAI-004 silent with description", ruleID: "VAI-004", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "Echoes the query.", inputSchema: z.object({ q: z.string() }), execute: async ({ q }) => q });
`},

	// VAI-005: dynamicTool is always untyped (fire); an empty z.object({}) is an
	// open schema (fire); a concrete Zod object is typed (silent).
	{name: "VAI-005 fires on dynamicTool", ruleID: "VAI-005", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { dynamicTool } from "ai";
import { z } from "zod";
export const t = dynamicTool({ description: "x", inputSchema: z.unknown(), execute: async (input) => input });
`},
	{name: "VAI-005 fires on empty z.object schema", ruleID: "VAI-005", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "x", inputSchema: z.object({}), execute: async () => 1 });
`},
	{name: "VAI-005 silent with typed zod schema", ruleID: "VAI-005", kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false, src: `
import { tool } from "ai";
import { z } from "zod";
export const t = tool({ description: "x", inputSchema: z.object({ city: z.string() }), execute: async ({ city }) => city });
`},

	// ─── CrewAI tool rules (CREW-*) ─────────────────────────────────────────
	{name: "CREW-001 fires on tool with no docstring", ruleID: "CREW-001", kind: models.KindCrewAITool, src: `
def search(q: str) -> str:
    return q
`, wantFires: true},
	{name: "CREW-001 silent with docstring", ruleID: "CREW-001", kind: models.KindCrewAITool, src: `
def search(q: str) -> str:
    """Search the web."""
    return q
`, wantFires: false},

	{name: "CREW-002 fires on untyped params", ruleID: "CREW-002", kind: models.KindCrewAITool, src: `
def search(q):
    """Search."""
    return q
`, wantFires: true},
	{name: "CREW-002 silent with typed params", ruleID: "CREW-002", kind: models.KindCrewAITool, src: `
def search(q: str) -> str:
    """Search."""
    return q
`, wantFires: false},
	{name: "CREW-002 silent with no params", ruleID: "CREW-002", kind: models.KindCrewAITool, src: `
def now() -> str:
    """No params, no problem."""
    return "ok"
`, wantFires: false},

	{name: "CREW-003 fires on eval", ruleID: "CREW-003", kind: models.KindCrewAITool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return eval(expr)
`, wantFires: true},
	{name: "CREW-003 silent without eval", ruleID: "CREW-003", kind: models.KindCrewAITool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return expr
`, wantFires: false},

	{name: "CREW-004 fires on subprocess", ruleID: "CREW-004", kind: models.KindCrewAITool, src: `
def run(cmd: str) -> str:
    """Run a command."""
    import subprocess
    return subprocess.run(cmd, shell=True)
`, wantFires: true},
	{name: "CREW-004 silent without shell", ruleID: "CREW-004", kind: models.KindCrewAITool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd
`, wantFires: false},

	{name: "CREW-005 fires on dynamic URL", ruleID: "CREW-005", kind: models.KindCrewAITool, src: `
def fetch(url: str) -> str:
    """Fetch a URL."""
    import requests
    return requests.get(url).text
`, wantFires: true},
	{name: "CREW-005 silent on literal URL", ruleID: "CREW-005", kind: models.KindCrewAITool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: false},

	{name: "CREW-006 fires on mutating tool without key", ruleID: "CREW-006", kind: models.KindCrewAITool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, wantFires: true},
	{name: "CREW-006 silent with idempotency key", ruleID: "CREW-006", kind: models.KindCrewAITool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, wantFires: false},

	{name: "CREW-108 fires on result_as_answer=True", ruleID: "CREW-108", kind: models.KindCrewAITool, src: `
def f(x: str) -> str:
    """Doc."""
    return x
`, toolConfig: map[string]string{"result_as_answer": "True"}, wantFires: true},
	{name: "CREW-108 silent without result_as_answer", ruleID: "CREW-108", kind: models.KindCrewAITool, src: `
def f(x: str) -> str:
    """Doc."""
    return x
`, toolConfig: map[string]string{}, wantFires: false},

	// ─── AutoGen tool rules (AG2-*) ─────────────────────────────────────────
	{name: "AG2-007 fires on tool with no docstring", ruleID: "AG2-007", kind: models.KindAutoGenTool, src: `
def search(q: str) -> str:
    return q
`, wantFires: true},
	{name: "AG2-007 silent with docstring", ruleID: "AG2-007", kind: models.KindAutoGenTool, src: `
def search(q: str) -> str:
    """Search the web."""
    return q
`, wantFires: false},

	{name: "AG2-008 fires on untyped params", ruleID: "AG2-008", kind: models.KindAutoGenTool, src: `
def search(q):
    """Search."""
    return q
`, wantFires: true},
	{name: "AG2-008 silent with typed params", ruleID: "AG2-008", kind: models.KindAutoGenTool, src: `
def search(q: str) -> str:
    """Search."""
    return q
`, wantFires: false},
	{name: "AG2-008 silent with no params", ruleID: "AG2-008", kind: models.KindAutoGenTool, src: `
def now() -> str:
    """No params, no problem."""
    return "ok"
`, wantFires: false},

	{name: "AG2-009 fires on subprocess", ruleID: "AG2-009", kind: models.KindAutoGenTool, src: `
def run(cmd: str) -> str:
    """Run a command."""
    import subprocess
    return subprocess.run(cmd, shell=True)
`, wantFires: true},
	{name: "AG2-009 silent without shell", ruleID: "AG2-009", kind: models.KindAutoGenTool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd
`, wantFires: false},

	{name: "AG2-010 fires on eval", ruleID: "AG2-010", kind: models.KindAutoGenTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return eval(expr)
`, wantFires: true},
	{name: "AG2-010 silent without eval", ruleID: "AG2-010", kind: models.KindAutoGenTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return expr
`, wantFires: false},

	{name: "AG2-011 fires on dynamic URL", ruleID: "AG2-011", kind: models.KindAutoGenTool, src: `
def fetch(url: str) -> str:
    """Fetch a URL."""
    import requests
    return requests.get(url).text
`, wantFires: true},
	{name: "AG2-011 silent on literal URL", ruleID: "AG2-011", kind: models.KindAutoGenTool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: false},

	{name: "AG2-012 fires on network call without timeout", ruleID: "AG2-012", kind: models.KindAutoGenTool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: true},
	{name: "AG2-012 silent with timeout", ruleID: "AG2-012", kind: models.KindAutoGenTool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data", timeout=10).text
`, wantFires: false},

	// ─── Pydantic AI tool rules (PYD-*) ─────────────────────────────────────
	{name: "PYD-001 fires on tool with no docstring", ruleID: "PYD-001", kind: models.KindPydanticAITool, src: `
def search(q: str) -> str:
    return q
`, wantFires: true},
	{name: "PYD-001 silent with docstring", ruleID: "PYD-001", kind: models.KindPydanticAITool, src: `
def search(q: str) -> str:
    """Search the web."""
    return q
`, wantFires: false},

	{name: "PYD-002 fires on untyped params", ruleID: "PYD-002", kind: models.KindPydanticAITool, src: `
def search(q):
    """Search."""
    return q
`, wantFires: true},
	{name: "PYD-002 silent with typed params", ruleID: "PYD-002", kind: models.KindPydanticAITool, src: `
def search(q: str) -> str:
    """Search."""
    return q
`, wantFires: false},
	{name: "PYD-002 silent with no params", ruleID: "PYD-002", kind: models.KindPydanticAITool, src: `
def now() -> str:
    """No params, no problem."""
    return "ok"
`, wantFires: false},

	{name: "PYD-003 fires on subprocess", ruleID: "PYD-003", kind: models.KindPydanticAITool, src: `
def run(cmd: str) -> str:
    """Run a command."""
    import subprocess
    return subprocess.run(cmd, shell=True)
`, wantFires: true},
	{name: "PYD-003 silent without shell", ruleID: "PYD-003", kind: models.KindPydanticAITool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd
`, wantFires: false},

	{name: "PYD-004 fires on eval", ruleID: "PYD-004", kind: models.KindPydanticAITool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return eval(expr)
`, wantFires: true},
	{name: "PYD-004 silent without eval", ruleID: "PYD-004", kind: models.KindPydanticAITool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return expr
`, wantFires: false},

	{name: "PYD-005 fires on dynamic URL", ruleID: "PYD-005", kind: models.KindPydanticAITool, src: `
def fetch(url: str) -> str:
    """Fetch a URL."""
    import requests
    return requests.get(url).text
`, wantFires: true},
	{name: "PYD-005 silent on literal URL", ruleID: "PYD-005", kind: models.KindPydanticAITool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: false},

	{name: "PYD-006 fires on network call without timeout", ruleID: "PYD-006", kind: models.KindPydanticAITool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data").text
`, wantFires: true},
	{name: "PYD-006 silent with timeout", ruleID: "PYD-006", kind: models.KindPydanticAITool, src: `
def fetch(q: str) -> str:
    """Fetch."""
    import requests
    return requests.get("https://api.example.com/data", timeout=10).text
`, wantFires: false},

	{name: "PYD-007 fires on mutating tool without key", ruleID: "PYD-007", kind: models.KindPydanticAITool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, wantFires: true},
	{name: "PYD-007 silent with idempotency key", ruleID: "PYD-007", kind: models.KindPydanticAITool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, wantFires: false},

	// ─── CSDK-001 missing docstring ─────────────────────────────────────────
	{name: "CSDK-001 fires on missing docstring", ruleID: "CSDK-001", kind: models.KindClaudeSDKTool, src: `
def fetch_data(x: str) -> dict:
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-001 silent with docstring", ruleID: "CSDK-001", kind: models.KindClaudeSDKTool, src: `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-002 untyped params ────────────────────────────────────────────
	{name: "CSDK-002 fires on untyped params", ruleID: "CSDK-002", kind: models.KindClaudeSDKTool, src: `
def fetch_data(x, y):
    """Does something."""
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-002 silent with typed params", ruleID: "CSDK-002", kind: models.KindClaudeSDKTool, src: `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`,
		toolConfig: nil, wantFires: false},
	{name: "CSDK-002 silent with no params", ruleID: "CSDK-002", kind: models.KindClaudeSDKTool, src: `
def fetch_data() -> dict:
    """No params, no problem."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-003 network without timeout ───────────────────────────────────
	{name: "CSDK-003 fires without timeout", ruleID: "CSDK-003", kind: models.KindClaudeSDKTool, src: `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id).json()
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-003 silent with timeout", ruleID: "CSDK-003", kind: models.KindClaudeSDKTool, src: `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id, timeout=10).json()
`,
		toolConfig: nil, wantFires: false},
	{name: "CSDK-003 silent on non-HTTP call", ruleID: "CSDK-003", kind: models.KindClaudeSDKTool, src: `
def get_data(cache_key: str) -> dict:
    """Read from cache."""
    return cache.fetch(cache_key)
`,
		toolConfig: nil, wantFires: false},
	{name: "CSDK-003 fires on session-alias get without timeout", ruleID: "CSDK-003", kind: models.KindClaudeSDKTool, src: `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    s = requests.Session()
    return s.get("https://api.example.com/invoice/" + id).json()
`,
		toolConfig: nil, wantFires: true},

	// ─── CSDK-004 unsafe path ───────────────────────────────────────────────
	{name: "CSDK-004 fires on path in open()", ruleID: "CSDK-004", kind: models.KindClaudeSDKTool, src: `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-004 silent with .resolve()", ruleID: "CSDK-004", kind: models.KindClaudeSDKTool, src: `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: false},
	{name: "CSDK-004 silent on non-pathish param", ruleID: "CSDK-004", kind: models.KindClaudeSDKTool, src: `
def get_editor(editor_id: str) -> dict:
    """Get editor config."""
    return {"id": editor_id}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-005 raw exceptions ────────────────────────────────────────────
	{name: "CSDK-005 fires on raise without try", ruleID: "CSDK-005", kind: models.KindClaudeSDKTool, src: `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-005 silent with try/except", ruleID: "CSDK-005", kind: models.KindClaudeSDKTool, src: `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-006 idempotency ───────────────────────────────────────────────
	{name: "CSDK-006 fires on mutating tool without key", ruleID: "CSDK-006", kind: models.KindClaudeSDKTool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-006 silent with idempotency key", ruleID: "CSDK-006", kind: models.KindClaudeSDKTool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: false},
	{name: "CSDK-006 silent on non-mutating name", ruleID: "CSDK-006", kind: models.KindClaudeSDKTool, src: `
def get_order(order_id: str) -> dict:
    """Fetch an order."""
    return {"id": order_id}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-007 ambiguous name ────────────────────────────────────────────
	{name: "CSDK-007 fires on ambiguous name", ruleID: "CSDK-007", kind: models.KindClaudeSDKTool, src: `
def process(data: dict) -> dict:
    """Process data."""
    return data
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-007 silent on descriptive name", ruleID: "CSDK-007", kind: models.KindClaudeSDKTool, src: `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-001 missing docstring ───────────────────────────────────────────
	{name: "OAI-001 fires on missing docstring", ruleID: "OAI-001", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-001 silent with docstring", ruleID: "OAI-001", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-002 untyped params ─────────────────────────────────────────────
	{name: "OAI-002 fires on untyped params", ruleID: "OAI-002", kind: models.KindOpenAITool, src: `
def fetch_data(x, y):
    """Does something."""
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-002 silent with typed params", ruleID: "OAI-002", kind: models.KindOpenAITool, src: `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-003 strict_mode=False ──────────────────────────────────────────
	{name: "OAI-003 fires when strict_mode=False in config", ruleID: "OAI-003", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`,
		toolConfig: map[string]string{"strict_mode": "False"}, wantFires: true},
	{name: "OAI-003 silent when strict_mode not set", ruleID: "OAI-003", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-004 no failure_error_function ──────────────────────────────────
	{name: "OAI-004 fires when failure_error_function absent", ruleID: "OAI-004", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-004 silent when failure_error_function present", ruleID: "OAI-004", kind: models.KindOpenAITool, src: `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`,
		toolConfig: map[string]string{"failure_error_function": "handle_error"}, wantFires: false},

	// ─── OAI-005 network without timeout ────────────────────────────────────
	{name: "OAI-005 fires without timeout", ruleID: "OAI-005", kind: models.KindOpenAITool, src: `
import requests
def get_data(id: str) -> dict:
    """Get data."""
    return requests.get("https://api.example.com/" + id).json()
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-005 silent with timeout", ruleID: "OAI-005", kind: models.KindOpenAITool, src: `
import requests
def get_data(id: str) -> dict:
    """Get data."""
    return requests.get("https://api.example.com/" + id, timeout=10).json()
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-006 unsafe path ────────────────────────────────────────────────
	{name: "OAI-006 fires on path in open()", ruleID: "OAI-006", kind: models.KindOpenAITool, src: `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-006 silent with .resolve()", ruleID: "OAI-006", kind: models.KindOpenAITool, src: `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-001 missing docstring on FunctionTool wrap ──────────────────────
	{name: "ADK-001 fires on missing docstring", ruleID: "ADK-001", kind: models.KindADKFunctionTool, src: `
def get_weather(city: str) -> str:
    return "sunny"
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-001 silent with docstring", ruleID: "ADK-001", kind: models.KindADKFunctionTool, src: `
def get_weather(city: str) -> str:
    """Look up the weather for a city."""
    return "sunny"
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-002 untyped params on FunctionTool wrap ─────────────────────────
	{name: "ADK-002 fires on untyped params", ruleID: "ADK-002", kind: models.KindADKFunctionTool, src: `
def get_weather(city):
    """Look up the weather."""
    return "sunny"
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-002 silent on typed params", ruleID: "ADK-002", kind: models.KindADKFunctionTool, src: `
def get_weather(city: str) -> str:
    """Look up the weather."""
    return "sunny"
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-003 network call without timeout ────────────────────────────────
	{name: "ADK-003 fires on requests.get without timeout", ruleID: "ADK-003", kind: models.KindADKFunctionTool, src: `
import requests

def get_weather(city: str) -> str:
    """Look up the weather."""
    return requests.get("https://api.example.com/w/" + city).text
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-003 silent with timeout", ruleID: "ADK-003", kind: models.KindADKFunctionTool, src: `
import requests

def get_weather(city: str) -> str:
    """Look up the weather."""
    return requests.get("https://api.example.com/w/" + city, timeout=10).text
`,
		toolConfig: nil, wantFires: false},

	// ─── alias + None coverage (OAI-005, ADK-003) ────────────────────────────
	{name: "OAI-005 fires on session-alias get without timeout", ruleID: "OAI-005", kind: models.KindOpenAITool, src: `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    s = requests.Session()
    return s.get(url).text
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-005 fires on timeout=None", ruleID: "OAI-005", kind: models.KindOpenAITool, src: `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    return requests.get(url, timeout=None).text
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-003 fires on session-alias get without timeout", ruleID: "ADK-003", kind: models.KindADKFunctionTool, src: `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    s = requests.Session()
    return s.get(url).text
`,
		toolConfig: nil, wantFires: true},

	// ─── OAI-007 ambiguous name ──────────────────────────────────────────────
	{name: "OAI-007 fires on ambiguous name", ruleID: "OAI-007", kind: models.KindOpenAITool, src: `
def process(data: dict) -> dict:
    """Process data."""
    return data
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-007 silent on descriptive name", ruleID: "OAI-007", kind: models.KindOpenAITool, src: `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-008 raw exceptions ──────────────────────────────────────────────
	{name: "OAI-008 fires on raise without try", ruleID: "OAI-008", kind: models.KindOpenAITool, src: `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-008 silent with try/except", ruleID: "OAI-008", kind: models.KindOpenAITool, src: `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-009 idempotency ─────────────────────────────────────────────────
	{name: "OAI-009 fires on mutating tool without key", ruleID: "OAI-009", kind: models.KindOpenAITool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-009 silent with idempotency key", ruleID: "OAI-009", kind: models.KindOpenAITool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-010 print to stdout ─────────────────────────────────────────────
	{name: "OAI-010 fires on print()", ruleID: "OAI-010", kind: models.KindOpenAITool, src: `
def fetch(x: str) -> dict:
    """Fetch."""
    print("debug", x)
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-010 silent without print", ruleID: "OAI-010", kind: models.KindOpenAITool, src: `
def fetch(x: str) -> dict:
    """Fetch."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-011 urllib without timeout ──────────────────────────────────────
	{name: "OAI-011 fires on urlopen without timeout", ruleID: "OAI-011", kind: models.KindOpenAITool, src: `
import urllib.request
def fetch(url: str) -> bytes:
    """Fetch."""
    return urllib.request.urlopen(url).read()
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-011 silent with timeout", ruleID: "OAI-011", kind: models.KindOpenAITool, src: `
import urllib.request
def fetch(url: str) -> bytes:
    """Fetch."""
    return urllib.request.urlopen(url, timeout=10).read()
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-012 subprocess spawn ────────────────────────────────────────────
	{name: "OAI-012 fires on subprocess.run", ruleID: "OAI-012", kind: models.KindOpenAITool, src: `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-012 silent without subprocess", ruleID: "OAI-012", kind: models.KindOpenAITool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd.upper()
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-013 dynamic code execution ──────────────────────────────────────
	{name: "OAI-013 fires on eval", ruleID: "OAI-013", kind: models.KindOpenAITool, src: `
def calc(expr: str):
    """Calc."""
    return eval(expr)
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-013 silent without eval/exec/compile", ruleID: "OAI-013", kind: models.KindOpenAITool, src: `
def calc(expr: str) -> int:
    """Calc."""
    return int(expr) + 1
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-004 unsafe path ─────────────────────────────────────────────────
	{name: "ADK-004 fires on path in open()", ruleID: "ADK-004", kind: models.KindADKFunctionTool, src: `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-004 silent with .resolve()", ruleID: "ADK-004", kind: models.KindADKFunctionTool, src: `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-005 raw exceptions ──────────────────────────────────────────────
	{name: "ADK-005 fires on raise without try", ruleID: "ADK-005", kind: models.KindADKFunctionTool, src: `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-005 silent with try/except", ruleID: "ADK-005", kind: models.KindADKFunctionTool, src: `
def process(x: str) -> dict:
    """Process x."""
    try:
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-006 idempotency ─────────────────────────────────────────────────
	{name: "ADK-006 fires on mutating tool without key", ruleID: "ADK-006", kind: models.KindADKFunctionTool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-006 silent with idempotency key", ruleID: "ADK-006", kind: models.KindADKFunctionTool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-007 ambiguous name ──────────────────────────────────────────────
	{name: "ADK-007 fires on ambiguous name", ruleID: "ADK-007", kind: models.KindADKFunctionTool, src: `
def handle(data: dict) -> dict:
    """Handle data."""
    return data
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-007 silent on descriptive name", ruleID: "ADK-007", kind: models.KindADKFunctionTool, src: `
def fetch_order(order_id: str) -> dict:
    """Fetch an order."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ADK-008 moved to agent scope (BashTool is a hosted tool on an LlmAgent) —
	// see policyAgentRuleCases.

	// ─── OAI-010 FP-safety: structured has_print_call ignores pprint ──────────
	{name: "OAI-010 silent on pprint (not the print builtin)", ruleID: "OAI-010", kind: models.KindOpenAITool, src: `
from pprint import pprint
def fetch(x: dict) -> dict:
    """Fetch."""
    pprint(x)
    return x
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-013 FP-safety: structured has_code_exec_call ignores re.compile ──
	{name: "OAI-013 silent on re.compile (not the compile builtin)", ruleID: "OAI-013", kind: models.KindOpenAITool, src: `
import re
def build(pattern: str):
    """Build."""
    return re.compile(pattern)
`,
		toolConfig: nil, wantFires: false},

	// ─── MCP pack (category mcp, applies_to [mcp_tool]) ──────────────────────
	// The mcp pack owns all mcp_tool coverage; the CSDK rules no longer list
	// mcp_tool. Each MCP rule needs a fire + silent case here.
	{name: "MCP-001 fires on missing description", ruleID: "MCP-001", kind: models.KindMCPTool, src: `
def search_docs(query: str) -> dict:
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-001 silent with docstring", ruleID: "MCP-001", kind: models.KindMCPTool, src: `
def search_docs(query: str) -> dict:
    """Search the docs."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-002 fires on untyped params", ruleID: "MCP-002", kind: models.KindMCPTool, src: `
def search_docs(query, limit):
    """Search."""
    return {}
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-002 silent with typed params", ruleID: "MCP-002", kind: models.KindMCPTool, src: `
def search_docs(query: str, limit: int) -> dict:
    """Search."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-003 fires on ambiguous name", ruleID: "MCP-003", kind: models.KindMCPTool, src: `
def process(data: dict) -> dict:
    """Process data."""
    return data
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-003 silent on descriptive name", ruleID: "MCP-003", kind: models.KindMCPTool, src: `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-004 fires without timeout", ruleID: "MCP-004", kind: models.KindMCPTool, src: `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id).json()
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-004 silent with timeout", ruleID: "MCP-004", kind: models.KindMCPTool, src: `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id, timeout=10).json()
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-005 fires on path in open()", ruleID: "MCP-005", kind: models.KindMCPTool, src: `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-005 silent with .resolve()", ruleID: "MCP-005", kind: models.KindMCPTool, src: `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-006 fires on raise without try", ruleID: "MCP-006", kind: models.KindMCPTool, src: `
def do_thing(x: str) -> dict:
    """Do a thing."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-006 silent with try/except", ruleID: "MCP-006", kind: models.KindMCPTool, src: `
def do_thing(x: str) -> dict:
    """Do a thing."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-007 fires on mutating tool without key", ruleID: "MCP-007", kind: models.KindMCPTool, src: `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-007 silent with idempotency key", ruleID: "MCP-007", kind: models.KindMCPTool, src: `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-008 fires on dynamic URL", ruleID: "MCP-008", kind: models.KindMCPTool, src: `
import requests
def fetch(path: str) -> dict:
    """Fetch."""
    return requests.get("https://api.example.com/" + path, timeout=5).json()
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-008 silent on literal URL", ruleID: "MCP-008", kind: models.KindMCPTool, src: `
import requests
def fetch_status() -> dict:
    """Fetch status."""
    return requests.get("https://api.example.com/status", timeout=5).json()
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-009 fires on eval", ruleID: "MCP-009", kind: models.KindMCPTool, src: `
def calc(expr: str) -> float:
    """Evaluate."""
    return eval(expr)
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-009 silent without eval", ruleID: "MCP-009", kind: models.KindMCPTool, src: `
import ast
def calc(expr: str) -> float:
    """Evaluate."""
    return ast.literal_eval(expr)
`,
		toolConfig: nil, wantFires: false},

	{name: "MCP-010 fires on subprocess", ruleID: "MCP-010", kind: models.KindMCPTool, src: `
import subprocess
def run_cmd(name: str) -> str:
    """Run a command."""
    return subprocess.run([name], capture_output=True).stdout.decode()
`,
		toolConfig: nil, wantFires: true},
	{name: "MCP-010 silent without subprocess", ruleID: "MCP-010", kind: models.KindMCPTool, src: `
def run_cmd(name: str) -> str:
    """Run a command."""
    return name
`,
		toolConfig: nil, wantFires: false},

	// ── MCP TypeScript rules (@modelcontextprotocol/sdk server authoring) ──
	{
		name: "MCP-011 fires on empty description", ruleID: "MCP-011",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"search\", { description: \"\", inputSchema: { q: z.string() } }, async ({ q }) => ({ content: [] }));\n",
	},
	{
		name: "MCP-011 silent when description present", ruleID: "MCP-011",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"search\", { description: \"Search the docs\", inputSchema: { q: z.string() } }, async ({ q }) => ({ content: [] }));\n",
	},
	{
		name: "MCP-012 fires on TS tool shelling out", ruleID: "MCP-012",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"run\", { description: \"run\", inputSchema: { cmd: z.string() } }, async ({ cmd }) => {\n" +
			"  const { execSync } = require(\"child_process\");\n" +
			"  execSync(cmd);\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	{
		name: "MCP-012 silent on pure tool", ruleID: "MCP-012",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"run\", { description: \"run\", inputSchema: { x: z.string() } }, async ({ x }) => ({ content: [{ type: \"text\", text: x }] }));\n",
	},
	{
		name: "MCP-013 fires on interpolated fetch URL", ruleID: "MCP-013",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"f\", { description: \"f\", inputSchema: { host: z.string() } }, async ({ host }) => {\n" +
			"  const r = await fetch(`https://${host}/api`);\n" +
			"  return { content: [{ type: \"text\", text: String(r.status) }] };\n" +
			"});\n",
	},
	{
		name: "MCP-013 silent on literal fetch URL", ruleID: "MCP-013",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"f\", { description: \"f\", inputSchema: { x: z.string() } }, async () => {\n" +
			"  const r = await fetch(\"https://example.com/api\");\n" +
			"  return { content: [{ type: \"text\", text: String(r.status) }] };\n" +
			"});\n",
	},
	{
		name: "MCP-014 fires on TS eval", ruleID: "MCP-014",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"calc\", { description: \"calc\", inputSchema: { e: z.string() } }, async ({ e }) => eval(e));\n",
	},
	{
		name: "MCP-014 silent without eval", ruleID: "MCP-014",
		kind: models.KindMCPTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n" +
			"const server = new McpServer({ name: \"s\", version: \"1.0.0\" });\n" +
			"server.registerTool(\"calc\", { description: \"calc\", inputSchema: { a: z.number() } }, async ({ a }) => ({ content: [{ type: \"text\", text: String(a + 1) }] }));\n",
	},

	// ─── OAI-014 privileged tool without needs_approval ──────────────────────
	{name: "OAI-014 fires on shell tool with no needs_approval", ruleID: "OAI-014", kind: models.KindOpenAITool, src: `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-014 silent when needs_approval is set", ruleID: "OAI-014", kind: models.KindOpenAITool, src: `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`,
		toolConfig: map[string]string{"needs_approval": "True"}, wantFires: false},
	{name: "OAI-014 silent on a tool that is not privileged", ruleID: "OAI-014", kind: models.KindOpenAITool, src: `
def echo(x: str) -> str:
    """Echo."""
    return x
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-015 failure_error_function=None ─────────────────────────────────
	{name: "OAI-015 fires when failure_error_function=None", ruleID: "OAI-015", kind: models.KindOpenAITool, src: `
def fetch(x: str) -> dict:
    """Fetch."""
    return {}
`,
		toolConfig: map[string]string{"failure_error_function": "None"}, wantFires: true},
	{name: "OAI-015 silent when failure_error_function is a real handler", ruleID: "OAI-015", kind: models.KindOpenAITool, src: `
def fetch(x: str) -> dict:
    """Fetch."""
    return {}
`,
		toolConfig: map[string]string{"failure_error_function": "handle_error"}, wantFires: false},
	{name: "OAI-015 silent when failure_error_function absent", ruleID: "OAI-015", kind: models.KindOpenAITool, src: `
def fetch(x: str) -> dict:
    """Fetch."""
    return {}
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-107 Claude tool body calls eval/exec/compile ───────────────────
	{name: "CSDK-107 fires on exec", ruleID: "CSDK-107", kind: models.KindClaudeSDKTool, src: `
def run(code: str):
    """Run."""
    exec(code)
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-107 silent without eval/exec/compile", ruleID: "CSDK-107", kind: models.KindClaudeSDKTool, src: `
def run(code: str) -> int:
    """Run."""
    return int(code) + 1
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-108 Claude tool body spawns a subprocess ───────────────────────
	{name: "CSDK-108 fires on subprocess.run", ruleID: "CSDK-108", kind: models.KindClaudeSDKTool, src: `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-108 silent without a shell call", ruleID: "CSDK-108", kind: models.KindClaudeSDKTool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd.upper()
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-009 SSRF: Claude tool fetches a caller-controlled URL ──────────
	{name: "CSDK-009 fires on requests.get with param URL", ruleID: "CSDK-009", kind: models.KindClaudeSDKTool, src: `
import requests
def fetch(url: str) -> str:
    """Fetch a URL."""
    return requests.get(url, timeout=10).text
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-009 silent on a literal URL", ruleID: "CSDK-009", kind: models.KindClaudeSDKTool, src: `
import requests
def fetch() -> str:
    """Fetch a fixed URL."""
    return requests.get("https://api.example.com/status", timeout=10).text
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-012 SSRF: ADK tool fetches a caller-controlled URL ──────────────
	{name: "ADK-012 fires on requests.get with param URL", ruleID: "ADK-012", kind: models.KindADKFunctionTool, src: `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    return requests.get(url, timeout=10).text
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-012 silent on a literal URL", ruleID: "ADK-012", kind: models.KindADKFunctionTool, src: `
import requests
def fetch() -> str:
    """Fetch."""
    return requests.get("https://api.example.com", timeout=10).text
`,
		toolConfig: nil, wantFires: false},

	// ─── OAI-018 SSRF (team rule): builds outbound URL from non-literal ──────
	{name: "OAI-018 fires on httpx.get with f-string URL", ruleID: "OAI-018", kind: models.KindOpenAITool, src: `
import httpx
def fetch(host: str) -> str:
    """Fetch."""
    return httpx.get(f"https://{host}/data", timeout=10).text
`,
		toolConfig: nil, wantFires: true},
	{name: "OAI-018 silent on a literal URL", ruleID: "OAI-018", kind: models.KindOpenAITool, src: `
import httpx
def fetch() -> str:
    """Fetch."""
    return httpx.get("https://api.example.com/data", timeout=10).text
`,
		toolConfig: nil, wantFires: false},

	// ─── CSDK-008 (team rule): **kwargs without explicit input_schema ────────
	// FunctionParams surfaces the **kwargs splat name, so the rule fires on a
	// real **kwargs signature (not only a plain param literally named kwargs).
	{name: "CSDK-008 fires on real **kwargs (no input_schema)", ruleID: "CSDK-008", kind: models.KindClaudeSDKTool, src: `
def configure(**kwargs):
    """Configure."""
    return kwargs
`,
		toolConfig: nil, wantFires: true},
	{name: "CSDK-008 silent on a typed param (no kwargs)", ruleID: "CSDK-008", kind: models.KindClaudeSDKTool, src: `
def configure(name: str):
    """Configure."""
    return name
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-009 (team rule): FunctionTool body prints to stdout ─────────────
	{name: "ADK-009 fires on print()", ruleID: "ADK-009", kind: models.KindADKFunctionTool, src: `
def report(x: str):
    """Report."""
    print(x)
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-009 silent without print()", ruleID: "ADK-009", kind: models.KindADKFunctionTool, src: `
def report(x: str) -> str:
    """Report."""
    return x.upper()
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-010 ADK tool body spawns a subprocess ──────────────────────────
	{name: "ADK-010 fires on subprocess.run", ruleID: "ADK-010", kind: models.KindADKFunctionTool, src: `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-010 silent without a shell call", ruleID: "ADK-010", kind: models.KindADKFunctionTool, src: `
def run(cmd: str) -> str:
    """Run."""
    return cmd.upper()
`,
		toolConfig: nil, wantFires: false},

	// ─── ADK-011 ADK tool body calls eval/exec/compile ──────────────────────
	{name: "ADK-011 fires on eval", ruleID: "ADK-011", kind: models.KindADKFunctionTool, src: `
def calc(expr: str):
    """Calc."""
    return eval(expr)
`,
		toolConfig: nil, wantFires: true},
	{name: "ADK-011 silent without eval/exec/compile", ruleID: "ADK-011", kind: models.KindADKFunctionTool, src: `
def calc(expr: str) -> int:
    """Calc."""
    return int(expr) + 1
`,
		toolConfig: nil, wantFires: false},

	// ── OAI-016: TS HTTP call without timeout (structural has_http_call_without_timeout) ──
	{
		name: "OAI-016 fires on TS fetch with no AbortSignal", ruleID: "OAI-016",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  const r = await fetch(\"https://example.com\");\n" +
			"  return r.status;\n" +
			"} });\n",
	},
	{
		name: "OAI-016 silent when AbortSignal present", ruleID: "OAI-016",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  const r = await fetch(\"https://example.com\", { signal: AbortSignal.timeout(15000) });\n" +
			"  return r.status;\n" +
			"} });\n",
	},
	{
		name: "OAI-016 fires when fetch options omit any timeout", ruleID: "OAI-016",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  const r = await fetch(\"https://example.com\", { method: \"POST\" });\n" +
			"  return r.status;\n" +
			"} });\n",
	},

	// ── VAI-011: Vercel AI tool HTTP call without timeout (structural) ──
	{
		name: "VAI-011 fires on a bare fetch in a Vercel tool", ruleID: "VAI-011",
		kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"ai\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool({ description: \"f\", inputSchema: z.object({ u: z.string() }), execute: async ({ u }) => {\n" +
			"  const r = await fetch(\"https://api.example.com\");\n" +
			"  return String(r.status);\n" +
			"} });\n",
	},
	{
		name: "VAI-011 silent when abortSignal timeout present", ruleID: "VAI-011",
		kind: models.KindVercelAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"ai\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool({ description: \"f\", inputSchema: z.object({ u: z.string() }), execute: async ({ u }) => {\n" +
			"  const r = await fetch(\"https://api.example.com\", { abortSignal: AbortSignal.timeout(10000) });\n" +
			"  return String(r.status);\n" +
			"} });\n",
	},

	// ── OAI-017: TS eval / new Function (has_body_text) ──
	{
		name: "OAI-017 fires on TS eval", ruleID: "OAI-017",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async (a) => {\n" +
			"  return eval(a.expr);\n" +
			"} });\n",
	},
	{
		name: "OAI-017 silent without eval", ruleID: "OAI-017",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  return 42;\n" +
			"} });\n",
	},

	// ── OAI-019: TS mutating tool name without idempotency (name_has_prefix + not has_body_text) ──
	{
		name: "OAI-019 fires on create_ tool", ruleID: "OAI-019",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"create_charge\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  return 1;\n" +
			"} });\n",
	},
	{
		name: "OAI-019 silent on read tool", ruleID: "OAI-019",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"get_status\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  return 1;\n" +
			"} });\n",
	},

	// ── OAI-022: TS tool has no description ──
	{
		name: "OAI-022 fires on empty description", ruleID: "OAI-022",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"\", parameters: {}, execute: async () => {\n" +
			"  return 1;\n" +
			"} });\n",
	},
	{
		name: "OAI-022 silent when description present", ruleID: "OAI-022",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"fetches the weather\", parameters: {}, execute: async () => {\n" +
			"  return 1;\n" +
			"} });\n",
	},

	// ── OAI-024: TS tool builds outbound URL from non-literal value (dynamic_url fact) ──
	{
		name: "OAI-024 fires on interpolated fetch URL", ruleID: "OAI-024",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async (a) => {\n" +
			"  const r = await fetch(`https://${a.host}/api`);\n" +
			"  return r.status;\n" +
			"} });\n",
	},
	{
		name: "OAI-024 silent on literal fetch URL", ruleID: "OAI-024",
		kind: models.KindOpenAITool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@openai/agents\";\n" +
			"export const t = tool({ name: \"f\", description: \"f\", parameters: {}, execute: async () => {\n" +
			"  const r = await fetch(\"https://example.com/api\");\n" +
			"  return r.status;\n" +
			"} });\n",
	},

	// ── ADK-013: TS FunctionTool has no description ──
	{
		name: "ADK-013 fires on empty description", ruleID: "ADK-013",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"sum\", description: \"\", parameters: { a: 0 }, execute: async ({ a }) => String(a) });\n",
	},
	{
		name: "ADK-013 silent when description present", ruleID: "ADK-013",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"sum\", description: \"adds two numbers\", parameters: { a: 0 }, execute: async ({ a }) => String(a) });\n",
	},

	// ── ADK-015: TS FunctionTool evaluates dynamic code (code_exec fact) ──
	{
		name: "ADK-015 fires on eval", ruleID: "ADK-015",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"calc\", description: \"calc\", parameters: { e: \"\" }, execute: async ({ e }) => eval(e) });\n",
	},
	{
		name: "ADK-015 silent without eval", ruleID: "ADK-015",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"calc\", description: \"calc\", parameters: { a: 0 }, execute: async ({ a }) => String(a + 1) });\n",
	},

	// ── ADK-016: TS FunctionTool fetches caller-controlled URL (dynamic_url fact) ──
	{
		name: "ADK-016 fires on interpolated fetch URL", ruleID: "ADK-016",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"f\", description: \"f\", parameters: { host: \"\" }, execute: async ({ host }) => { const r = await fetch(`https://${host}/api`); return r.status; } });\n",
	},
	{
		name: "ADK-016 silent on literal fetch URL", ruleID: "ADK-016",
		kind: models.KindADKFunctionTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { FunctionTool } from \"@google/adk\";\n" +
			"const t = new FunctionTool({ name: \"f\", description: \"f\", parameters: { a: 0 }, execute: async () => { const r = await fetch(\"https://example.com/api\"); return r.status; } });\n",
	},

	// ── CSDK-010 shell ──
	{
		name: "CSDK-010 fires on TS tool shelling out", ruleID: "CSDK-010",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"run\", \"runs\", { cmd: z.string() }, async ({ cmd }) => {\n" +
			"  const { execSync } = require(\"child_process\");\n" +
			"  execSync(cmd);\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	{
		name: "CSDK-010 silent on pure tool", ruleID: "CSDK-010",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"add\", \"adds\", { a: z.number() }, async ({ a }) => {\n" +
			"  return { content: [{ type: \"text\", text: String(a + 1) }] };\n" +
			"});\n",
	},
	// ── CSDK-011 code-exec ──
	{
		name: "CSDK-011 fires on TS eval", ruleID: "CSDK-011",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"calc\", \"calc\", { e: z.string() }, async ({ e }) => {\n" +
			"  return { content: [{ type: \"text\", text: String(eval(e)) }] };\n" +
			"});\n",
	},
	{
		name: "CSDK-011 silent without eval", ruleID: "CSDK-011",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"calc\", \"calc\", { a: z.number() }, async ({ a }) => {\n" +
			"  return { content: [{ type: \"text\", text: String(a) }] };\n" +
			"});\n",
	},
	{
		name: "CSDK-011 fires on new Function(", ruleID: "CSDK-011",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"run\", \"runs\", { b: z.string() }, async ({ b }) => {\n" +
			"  const fn = new Function(\"return \" + b);\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	{
		name: "CSDK-011 silent on retrieval( (no false eval match)", ruleID: "CSDK-011",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"search\", \"searches\", { q: z.string() }, async ({ q }) => {\n" +
			"  const r = await retrieval(q);\n" +
			"  return { content: [{ type: \"text\", text: r }] };\n" +
			"});\n",
	},
	// ── CSDK-012 fs-write ──
	{
		name: "CSDK-012 fires on TS writeFileSync", ruleID: "CSDK-012",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"save\", \"saves\", { p: z.string(), d: z.string() }, async ({ p, d }) => {\n" +
			"  const fs = require(\"fs\");\n" +
			"  fs.writeFileSync(p, d);\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	{
		name: "CSDK-012 silent without write", ruleID: "CSDK-012",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"read\", \"reads\", { p: z.string() }, async ({ p }) => {\n" +
			"  return { content: [{ type: \"text\", text: p }] };\n" +
			"});\n",
	},
	// ── CSDK-013 SSRF (dynamic_url fact) ──
	{
		name: "CSDK-013 fires on interpolated fetch URL", ruleID: "CSDK-013",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"f\", \"f\", { host: z.string() }, async ({ host }) => {\n" +
			"  await fetch(`https://${host}/api`);\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	{
		name: "CSDK-013 silent on literal fetch URL", ruleID: "CSDK-013",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"f\", \"f\", {}, async () => {\n" +
			"  await fetch(\"https://example.com/api\");\n" +
			"  return { content: [] };\n" +
			"});\n",
	},
	// ── CSDK-014 tool has no description ──
	{
		name: "CSDK-014 fires on TS tool with empty description", ruleID: "CSDK-014",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"save\", \"\", { p: z.string() }, async ({ p }) => {\n" +
			"  return { content: [{ type: \"text\", text: p }] };\n" +
			"});\n",
	},
	{
		name: "CSDK-014 silent when description present", ruleID: "CSDK-014",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"save\", \"saves a note to disk\", { p: z.string() }, async ({ p }) => {\n" +
			"  return { content: [{ type: \"text\", text: p }] };\n" +
			"});\n",
	},
	// ── CSDK-016 mutating tool has no idempotency key ──
	{
		name: "CSDK-016 fires on camelCase mutating tool with no idempotency param", ruleID: "CSDK-016",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: true,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"createCharge\", \"charges a card\", { amount: z.number() }, async ({ amount }) => {\n" +
			"  return { content: [{ type: \"text\", text: String(amount) }] };\n" +
			"});\n",
	},
	{
		name: "CSDK-016 silent when idempotency key present", ruleID: "CSDK-016",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"createCharge\", \"charges a card\", { amount: z.number(), idempotencyKey: z.string() }, async ({ amount }) => {\n" +
			"  return { content: [{ type: \"text\", text: String(amount) }] };\n" +
			"});\n",
	},
	{
		name: "CSDK-016 silent on non-mutating tool name", ruleID: "CSDK-016",
		kind: models.KindClaudeSDKTool, lang: models.LanguageTypeScript, wantFires: false,
		src: "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"import { z } from \"zod\";\n" +
			"export const t = tool(\"getBalance\", \"reads balance\", { id: z.string() }, async ({ id }) => {\n" +
			"  return { content: [{ type: \"text\", text: id }] };\n" +
			"});\n",
	},
}

// policyRepoRuleCases covers repo-scoped rules.
var policyRepoRuleCases = []policyRepoCase{
	// ─── LangChain repo rule (LC-201) ───────────────────────────────────────
	{"LC-201 fires when LangChain code has no agent-guidance doc", "LC-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKLangChain}},
		true},
	{"LC-201 silent when AGENTS.md present", "LC-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKLangChain}},
		false},
	// ─── CrewAI repo rule (CREW-201) ─────────────────────────────────────────
	{"CREW-201 fires when CrewAI code has no agent-guidance doc", "CREW-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKCrewAI}},
		true},
	{"CREW-201 silent when AGENTS.md present", "CREW-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKCrewAI}},
		false},
	// ─── AutoGen repo rule (AG2-201) ─────────────────────────────────────────
	{"AG2-201 fires when AutoGen code has no agent-guidance doc", "AG2-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKAutoGen}},
		true},
	{"AG2-201 silent when AGENTS.md present", "AG2-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKAutoGen}},
		false},
	// ─── Pydantic AI repo rule (PYD-201) ─────────────────────────────────────
	{"PYD-201 fires when Pydantic AI code has no agent-guidance doc", "PYD-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKPydanticAI}},
		true},
	{"PYD-201 silent when AGENTS.md present", "PYD-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKPydanticAI}},
		false},
	// ─── Vercel AI repo rule (VAI-012) ───────────────────────────────────────
	{"VAI-012 fires when Vercel AI code has no agent-guidance doc", "VAI-012",
		models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKVercelAI}},
		true},
	{"VAI-012 silent when CLAUDE.md present", "VAI-012",
		models.RepoProfile{
			Languages: []models.Language{models.LanguageTypeScript},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentClaudeMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKVercelAI}},
		false},
	// ─── OAI-201 default tracing (repo-scoped) ───────────────────────────────
	{"OAI-201 fires when using default tracing", "OAI-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKOpenAIAgents},
			UsesDefaultTracing: true,
		},
		true},
	{"OAI-201 silent when custom tracing configured", "OAI-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKOpenAIAgents},
			UsesDefaultTracing: false,
		},
		false},
	// Language gate: a TS-only repo using @openai/agents must NOT fire
	// OAI-201 even though SDKsDetected contains openai_agents and the
	// (Python-shaped) default-tracing predicate trivially holds — the rule
	// declares language: python and the repo has no Python.
	{"OAI-201 silent on TS-only OpenAI repo (language gate)", "OAI-201",
		models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKOpenAIAgents},
			UsesDefaultTracing: true,
		},
		false},

	// ─── CSDK-201 defaultMode bypass (repo-scoped) ───────────────────────────
	{"CSDK-201 fires when defaultMode is bypassPermissions", "CSDK-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:   []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeSettings: []models.ClaudeSettings{{DefaultMode: "bypassPermissions"}},
		},
		true},
	{"CSDK-201 silent when defaultMode is default", "CSDK-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:   []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeSettings: []models.ClaudeSettings{{DefaultMode: "default"}},
		},
		false},
	// acceptEdits is a real mode but intentionally NOT flagged by CSDK-201 —
	// the rule is scoped to the genuinely-dangerous full bypass only.
	{"CSDK-201 silent when defaultMode is acceptEdits", "CSDK-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:   []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeSettings: []models.ClaudeSettings{{DefaultMode: "acceptEdits"}},
		},
		false},
	// No settings at all (e.g. SDK present via code only): nothing to flag.
	{"CSDK-201 silent when no settings declare a defaultMode", "CSDK-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:   []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeSettings: []models.ClaudeSettings{{}},
		},
		false},

	// ─── CSDK-202 session permission_mode bypass (repo-scoped) ───────────────
	{"CSDK-202 fires when ClaudeAgentOptions permission_mode is bypassPermissions", "CSDK-202",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeAgentOptions: []models.ClaudeAgentOptionsDef{optionsWithPermissionMode("bypassPermissions")},
		},
		true},
	{"CSDK-202 silent when permission_mode is default", "CSDK-202",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeAgentOptions: []models.ClaudeAgentOptionsDef{optionsWithPermissionMode("default")},
		},
		false},
	// permission_mode absent from the options object: nothing to flag.
	{"CSDK-202 silent when no permission_mode set", "CSDK-202",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKClaudeAgentSDK},
			ClaudeAgentOptions: []models.ClaudeAgentOptionsDef{{}},
		},
		false},

	// ─── CSDK-203 / ADK-201 / OAI-202 (team rules): SDK code but no agent doc ─
	// repo_has_sdk_in_code reads inv.SDKsDetected; repo_component_present reads
	// profile.Manifest.Components. Fire = SDK present AND neither an agents_md
	// NOR a claude_md component. Either AGENTS.md (vendor-neutral) or CLAUDE.md
	// silences the rule.
	{"CSDK-203 fires when Claude SDK code has no agent-guidance doc", "CSDK-203",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}},
		true},
	{"CSDK-203 silent when CLAUDE.md present", "CSDK-203",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentClaudeMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}},
		false},
	{"CSDK-203 silent when AGENTS.md present", "CSDK-203",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKClaudeAgentSDK}},
		false},
	{"ADK-201 fires when ADK code has no agent-guidance doc", "ADK-201",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKGoogleADK}},
		true},
	{"ADK-201 silent when CLAUDE.md present", "ADK-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentClaudeMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKGoogleADK}},
		false},
	{"ADK-201 silent when AGENTS.md present", "ADK-201",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKGoogleADK}},
		false},
	{"OAI-202 fires when OpenAI code has no agent-guidance doc", "OAI-202",
		models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}},
		true},
	{"OAI-202 silent when CLAUDE.md present", "OAI-202",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentClaudeMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}},
		false},
	{"OAI-202 silent when AGENTS.md present", "OAI-202",
		models.RepoProfile{
			Languages: []models.Language{models.LanguagePython},
			Manifest:  models.ScanManifest{Components: []models.AgentComponent{{Kind: models.ComponentAgentsMd}}},
		},
		models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}},
		false},
}

// optionsWithPermissionMode builds a ClaudeAgentOptionsDef whose captured
// kwargs contain permission_mode set to the given string literal, mirroring
// what DiscoverClaudeAgentOptions produces from
// ClaudeAgentOptions(permission_mode="...").
func optionsWithPermissionMode(mode string) models.ClaudeAgentOptionsDef {
	return models.ClaudeAgentOptionsDef{
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"permission_mode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"` + mode + `"`}},
			},
		},
	}
}

// policySubagentRuleCases covers subagent-scoped rules.
var policySubagentRuleCases = []policySubagentCase{
	{"CSDK-110 fires when subagent grants Bash", "CSDK-110",
		models.SubagentDef{Name: "inbox-searcher", Location: models.Location{FilePath: ".claude/agents/inbox-searcher.md"},
			Tools: []string{"Read", "Bash", "Grep"}}, models.RepoInventory{}, true},
	{"CSDK-110 silent when no Bash", "CSDK-110",
		models.SubagentDef{Name: "reader", Location: models.Location{FilePath: ".claude/agents/reader.md"},
			Tools: []string{"Read", "Grep"}}, models.RepoInventory{}, false},

	{"CSDK-111 fires when subagent grants Write", "CSDK-111",
		models.SubagentDef{Name: "editor", Location: models.Location{FilePath: ".claude/agents/editor.md"},
			Tools: []string{"Read", "Write"}}, models.RepoInventory{}, true},
	{"CSDK-111 fires when subagent grants WebFetch", "CSDK-111",
		models.SubagentDef{Name: "fetcher", Location: models.Location{FilePath: ".claude/agents/fetcher.md"},
			Tools: []string{"Read", "WebFetch"}}, models.RepoInventory{}, true},
	{"CSDK-111 silent on read-only tool set", "CSDK-111",
		models.SubagentDef{Name: "reader", Location: models.Location{FilePath: ".claude/agents/reader2.md"},
			Tools: []string{"Read", "Grep", "Glob"}}, models.RepoInventory{}, false},
}

// policyAgentRuleCases covers agent-scoped rules.
var policyAgentRuleCases = []policyAgentCase{
	// ─── LangChain agent rules (LC-*) ───────────────────────────────────────
	{"LC-101 fires when agent wires PythonREPLTool", "LC-101",
		models.AgentDef{
			SDK: models.SDKLangChain, Class: "ReactAgent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "PythonREPLTool"}},
		},
		models.RepoInventory{}, true},
	{"LC-101 silent without a dangerous builtin", "LC-101",
		models.AgentDef{SDK: models.SDKLangChain, Class: "ReactAgent", Language: models.LanguagePython},
		models.RepoInventory{}, false},

	{"LC-102 fires when AgentExecutor has no max_iterations", "LC-102",
		models.AgentDef{SDK: models.SDKLangChain, Class: "AgentExecutor", Language: models.LanguagePython},
		models.RepoInventory{}, true},
	{"LC-102 silent when max_iterations set", "LC-102",
		models.AgentDef{SDK: models.SDKLangChain, Class: "AgentExecutor", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"max_iterations": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "5"}},
			}}},
		models.RepoInventory{}, false},

	{"LC-111 fires when TS AgentExecutor has no maxIterations", "LC-111",
		models.AgentDef{SDK: models.SDKLangChain, Class: "AgentExecutor", Language: models.LanguageTypeScript},
		models.RepoInventory{}, true},
	{"LC-111 silent when maxIterations set", "LC-111",
		models.AgentDef{SDK: models.SDKLangChain, Class: "AgentExecutor", Language: models.LanguageTypeScript,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"maxIterations": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "5"}},
			}}},
		models.RepoInventory{}, false},

	// ─── CrewAI agent rules (CREW-*) ────────────────────────────────────────
	{"CREW-101 fires when allow_code_execution=True", "CREW-101",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"allow_code_execution": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
			}}},
		models.RepoInventory{}, true},
	{"CREW-101 silent when allow_code_execution=False", "CREW-101",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"allow_code_execution": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
			}}},
		models.RepoInventory{}, false},

	{"CREW-102 fires when code_execution_mode=unsafe", "CREW-102",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_mode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"unsafe"`}},
			}}},
		models.RepoInventory{}, true},
	{"CREW-102 silent when code_execution_mode=safe (default)", "CREW-102",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_mode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"safe"`}},
			}}},
		models.RepoInventory{}, false},

	{"CREW-103 fires when agent wires CodeInterpreterTool", "CREW-103",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "CodeInterpreterTool"}},
		},
		models.RepoInventory{}, true},
	{"CREW-103 silent without the code interpreter", "CREW-103",
		models.AgentDef{SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython},
		models.RepoInventory{}, false},

	{"CREW-104 fires when allow_delegation=True", "CREW-104",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"allow_delegation": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
			}}},
		models.RepoInventory{}, true},
	{"CREW-104 silent when allow_delegation=False", "CREW-104",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"allow_delegation": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
			}}},
		models.RepoInventory{}, false},

	{"CREW-106 fires when FileReadTool has no file_path", "CREW-106",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "FileReadTool", Resolved: &models.HostedToolDef{Class: "FileReadTool"}},
			}},
		models.RepoInventory{}, true},
	{"CREW-106 silent when FileReadTool pins file_path", "CREW-106",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "FileReadTool", Resolved: &models.HostedToolDef{Class: "FileReadTool",
					Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
						"file_path": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"data.txt"`}},
					}}}},
			}},
		models.RepoInventory{}, false},

	{"CREW-107 fires when agent wires ScrapeWebsiteTool", "CREW-107",
		models.AgentDef{
			SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "ScrapeWebsiteTool"}},
		},
		models.RepoInventory{}, true},
	{"CREW-107 silent without a URL-fetching builtin", "CREW-107",
		models.AgentDef{SDK: models.SDKCrewAI, Class: "Agent", Language: models.LanguagePython},
		models.RepoInventory{}, false},

	// ─── AutoGen agent rules (AG2-*) ────────────────────────────────────────
	// AG2-001: code_execution_config={"use_docker": False} — nested kwarg path.
	{"AG2-001 fires when use_docker=False", "AG2-001",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
				}},
			}}},
		models.RepoInventory{}, true},
	{"AG2-001 silent when use_docker=True", "AG2-001",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
			}}},
		models.RepoInventory{}, false},

	// AG2-002: human_input_mode=NEVER AND code_execution_config present.
	{"AG2-002 fires when human_input_mode=NEVER with code exec", "AG2-002",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"human_input_mode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"NEVER"`}},
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
			}}},
		models.RepoInventory{}, true},
	{"AG2-002 silent when human_input_mode=ALWAYS", "AG2-002",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"human_input_mode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"ALWAYS"`}},
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
			}}},
		models.RepoInventory{}, false},

	// AG2-004: GroupChatManager / GroupChat with no max_round.
	{"AG2-004 fires when GroupChatManager has no max_round", "AG2-004",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "GroupChatManager", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}}},
		models.RepoInventory{}, true},
	{"AG2-004 silent when max_round is set", "AG2-004",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "GroupChat", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"max_round": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "12"}},
			}}},
		models.RepoInventory{}, false},

	// AG2-005: AssistantAgent with code_execution_config present.
	{"AG2-005 fires when AssistantAgent enables code exec", "AG2-005",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "AssistantAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
			}}},
		models.RepoInventory{}, true},
	{"AG2-005 silent when AssistantAgent has no code exec", "AG2-005",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "AssistantAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"llm_config": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "llm_config"}},
			}}},
		models.RepoInventory{}, false},

	// AG2-006: code_execution_config present AND no max_consecutive_auto_reply.
	{"AG2-006 fires when code exec has no auto-reply cap", "AG2-006",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
			}}},
		models.RepoInventory{}, true},
	{"AG2-006 silent when max_consecutive_auto_reply is set", "AG2-006",
		models.AgentDef{
			SDK: models.SDKAutoGen, Class: "UserProxyAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"code_execution_config": {Children: map[string]*models.KwargTree{
					"use_docker": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
				}},
				"max_consecutive_auto_reply": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "3"}},
			}}},
		models.RepoInventory{}, false},

	// ─── OAI-101 no input_guardrails + shell tools ────────────────────────────
	{"OAI-101 fires when no guardrails and has shell tool", "OAI-101",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			ToolRefs: []models.ToolRef{{Name: "run_cmd", Resolved: &models.ToolDef{Kind: models.KindShellInvocation}}},
		},
		models.RepoInventory{},
		true},
	// E2: a hosted ShellTool (no ToolDef) now satisfies the shell-tool clause.
	{"OAI-101 fires when no guardrails and has a hosted ShellTool", "OAI-101",
		models.AgentDef{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}},
		},
		models.RepoInventory{},
		true},
	{"OAI-101 silent when input_guardrails present", "OAI-101",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"input_guardrails": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_guard"},
				}}},
			}},
			ToolRefs: []models.ToolRef{{Name: "run_cmd", Resolved: &models.ToolDef{Kind: models.KindShellInvocation}}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-102 stop_on_first_tool ──────────────────────────────────────────
	{"OAI-102 fires on stop_on_first_tool", "OAI-102",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"tool_use_behavior": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"stop_on_first_tool"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"OAI-102 silent on default behavior", "OAI-102",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs:   &models.KwargTree{Children: map[string]*models.KwargTree{}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-103 tool_choice=required + reset_tool_choice=False ──────────────
	{"OAI-103 fires on loop pattern", "OAI-103",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"model_settings": {Children: map[string]*models.KwargTree{
					"tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"required"`}},
				}},
				"reset_tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
			}},
		},
		models.RepoInventory{},
		true},
	{"OAI-103 silent when reset_tool_choice not set", "OAI-103",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"model_settings": {Children: map[string]*models.KwargTree{
					"tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"required"`}},
				}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-104 raw Agent with shell tools ──────────────────────────────────
	{"OAI-104 fires on Agent class with shell tool", "OAI-104",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			ToolRefs: []models.ToolRef{{Name: "run_cmd", Resolved: &models.ToolDef{Kind: models.KindShellInvocation}}},
		},
		models.RepoInventory{},
		true},
	{"OAI-104 silent on Agent with no shell tools", "OAI-104",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			ToolRefs: []models.ToolRef{{Name: "fetch", Resolved: &models.ToolDef{Kind: models.KindOpenAITool}}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-106 mcp_servers + no input_guardrails ───────────────────────────
	{"OAI-106 fires with mcp_servers and no guardrails", "OAI-106",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"mcp_servers": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_mcp"},
				}}},
			}},
		},
		models.RepoInventory{},
		true},
	{"OAI-106 silent when input_guardrails also present", "OAI-106",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"mcp_servers": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_mcp"},
				}}},
				"input_guardrails": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_guard"},
				}}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── CSDK-101 Claude subagent granted Bash ────────────────────────────────
	{"CSDK-101 fires when AgentDefinition grants Bash", "CSDK-101",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "data-analyst",
			ToolRefs: []models.ToolRef{
				{Name: `"Glob"`, External: true},
				{Name: `"Bash"`, External: true},
				{Name: `"Write"`, External: true},
			},
		},
		models.RepoInventory{},
		true},
	{"CSDK-101 silent when no Bash in tools", "CSDK-101",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "researcher",
			ToolRefs: []models.ToolRef{
				{Name: `"WebSearch"`, External: true},
				{Name: `"Write"`, External: true},
			},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-101 LlmAgent with no description ────────────────────────────────
	{"ADK-101 fires when LlmAgent has no description", "ADK-101",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "child",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"child"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-101 silent when LlmAgent has description", "ADK-101",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "child",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":        {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"child"`}},
				"description": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"Looks up weather."`}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-102 BashTool without before_tool_callback ───────────────────────
	{"ADK-102 fires with BashTool and no before_tool_callback", "ADK-102",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-102 silent when before_tool_callback is present", "ADK-102",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "my_guard"}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-103 sub-agent granted BashTool ──────────────────────────────────
	{"ADK-103 fires on sub-agent with BashTool", "ADK-103",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Location:       models.Location{FilePath: "main.py"},
			Name:           "child",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
		},
		models.RepoInventory{Agents: []models.AgentDef{
			{
				SDK:      models.SDKGoogleADK,
				Class:    "SequentialAgent",
				Language: models.LanguagePython,
				Location: models.Location{FilePath: "main.py"},
				Name:     "parent",
				HandoffRefs: []models.AgentRef{
					{Name: "child", Resolved: &models.AgentDef{Name: "child", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython}},
				},
			},
		}},
		true},
	{"ADK-103 silent on root agent (not a sub-agent of any)", "ADK-103",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Location:       models.Location{FilePath: "main.py"},
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
		},
		models.RepoInventory{Agents: []models.AgentDef{
			{
				SDK:      models.SDKGoogleADK,
				Class:    "LlmAgent",
				Language: models.LanguagePython,
				Location: models.Location{FilePath: "main.py"},
				Name:     "sibling",
			},
		}},
		false},

	// ─── ADK-102 before_tool_callback=None counts as missing ─────────────────
	{"ADK-102 fires when before_tool_callback is None", "ADK-102",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprLiteralNone, Text: "None"}},
			}},
		},
		models.RepoInventory{},
		true},

	// ─── CSDK-102 Claude subagent granted WebSearch ──────────────────────────
	// Claude AgentDefinition tools are string literals → ToolRefs (Name carries
	// the quoted token), NOT HostedToolRefs. The rule uses agent_grants_builtin_tool.
	{"CSDK-102 fires when AgentDefinition grants WebSearch", "CSDK-102",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "researcher",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"WebSearch"`, External: true},
			},
		},
		models.RepoInventory{},
		true},
	{"CSDK-102 silent when no WebSearch granted", "CSDK-102",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "writer",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"Grep"`, External: true},
			},
		},
		models.RepoInventory{},
		false},

	// ─── CSDK-103 permissionMode=bypassPermissions ───────────────────────────
	{"CSDK-103 fires on bypassPermissions", "CSDK-103",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "worker",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"permissionMode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"bypassPermissions"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"CSDK-103 silent on default permission mode", "CSDK-103",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "worker",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"permissionMode": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"default"`}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── CSDK-104 subagent granted Write/Edit ─────────────────────────────────
	{"CSDK-104 fires when AgentDefinition grants Edit", "CSDK-104",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "coder",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"Edit"`, External: true},
			},
		},
		models.RepoInventory{},
		true},
	{"CSDK-104 silent on read-only tool set", "CSDK-104",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "reader",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"Grep"`, External: true},
			},
		},
		models.RepoInventory{},
		false},

	// ─── CSDK-105 subagent granted WebFetch ──────────────────────────────────
	{"CSDK-105 fires when AgentDefinition grants WebFetch", "CSDK-105",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "fetcher",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"WebFetch"`, External: true},
			},
		},
		models.RepoInventory{},
		true},
	{"CSDK-105 silent without WebFetch", "CSDK-105",
		models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguagePython,
			Name:     "reader",
			ToolRefs: []models.ToolRef{
				{Name: `"Read"`, External: true},
				{Name: `"WebSearch"`, External: true},
			},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-109 WebSearchTool without input_guardrails ──────────────────────
	{"OAI-109 fires with WebSearchTool and no guardrails", "OAI-109",
		models.AgentDef{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
		},
		models.RepoInventory{},
		true},
	{"OAI-109 silent when input_guardrails present", "OAI-109",
		models.AgentDef{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"input_guardrails": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_guard"},
				}}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-104 LlmAgent without generate_content_config.safety_settings ────
	// safety_settings is NOT a top-level LlmAgent kwarg — it lives inside
	// generate_content_config (a google-genai GenerateContentConfig). The match
	// is the dotted path generate_content_config.safety_settings; discovery
	// descends into the nested constructor call (extractCallKwargs/exprFromNode).
	{"ADK-104 fires when generate_content_config has no safety_settings", "ADK-104",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"generate_content_config": {
					Value: &models.Expr{Kind: models.ExprCall, Text: "types.GenerateContentConfig(temperature=0.2)"},
					Children: map[string]*models.KwargTree{
						"temperature": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "0"}},
					},
				},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-104 fires when no generate_content_config at all", "ADK-104",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-104 silent when generate_content_config.safety_settings present", "ADK-104",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"generate_content_config": {
					Value: &models.Expr{Kind: models.ExprCall, Text: "types.GenerateContentConfig(safety_settings=safety)"},
					Children: map[string]*models.KwargTree{
						"safety_settings": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "safety"}},
					},
				},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-105 web search built-in without before_tool_callback ────────────
	// google_search as a CALL (GoogleSearchTool()) → HostedToolRef Class
	// "GoogleSearchTool"; as a bare instance → ToolRef Name "google_search".
	// Discovery never emits a HostedToolRef "google_search", so the rule matches
	// both real shapes via an any: branch.
	{"ADK-105 fires with GoogleSearchTool hosted class, no before_tool_callback", "ADK-105",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "GoogleSearchTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-105 fires with google_search instance (ToolRef), no before_tool_callback", "ADK-105",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			ToolRefs: []models.ToolRef{{Name: "google_search", External: true}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-105 silent when before_tool_callback present", "ADK-105",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "GoogleSearchTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "my_guard"}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-106 code_executor without before_model_callback ─────────────────
	{"ADK-106 fires with code_executor and no before_model_callback", "ADK-106",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":          {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"code_executor": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "UnsafeLocalCodeExecutor()"}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-106 silent when before_model_callback present", "ADK-106",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                  {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"code_executor":         {Value: &models.Expr{Kind: models.ExprNameRef, Text: "executor"}},
				"before_model_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "guard"}},
			}},
		},
		models.RepoInventory{},
		false},
	{"ADK-106 silent when no code_executor", "ADK-106",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "root",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-107 AgentTool without before_tool_callback ──────────────────────
	{"ADK-107 fires with AgentTool and no before_tool_callback", "ADK-107",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "AgentTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-107 silent when before_tool_callback present", "ADK-107",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "AgentTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "guard"}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-108 LoopAgent without max_iterations ────────────────────────────
	{"ADK-108 fires when LoopAgent has no max_iterations", "ADK-108",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LoopAgent",
			Language: models.LanguagePython,
			Name:     "loop",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"loop"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-108 silent when max_iterations set", "ADK-108",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LoopAgent",
			Language: models.LanguagePython,
			Name:     "loop",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":           {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"loop"`}},
				"max_iterations": {Value: &models.Expr{Kind: models.ExprLiteralInt, Text: "5"}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-110 UrlContextTool/LoadWebPage without before_tool_callback ─────
	{"ADK-110 fires with UrlContextTool and no before_tool_callback", "ADK-110",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "UrlContextTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-110 silent when before_tool_callback present", "ADK-110",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "LoadWebPage"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "guard"}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── ADK-008 BashTool without a restrictive policy (E1 hosted-kwarg) ─────
	{"ADK-008 fires when BashTool has no policy", "ADK-008",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "ops",
			HostedToolRefs: []models.HostedToolRef{
				{Class: "BashTool", Resolved: &models.HostedToolDef{Class: "BashTool"}},
			},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"ops"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-008 silent when BashTool has a policy", "ADK-008",
		models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Name:     "ops",
			HostedToolRefs: []models.HostedToolRef{
				{Class: "BashTool", Resolved: &models.HostedToolDef{Class: "BashTool",
					Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
						"policy": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "my_policy"}},
					}}}},
			},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"ops"`}},
			}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-111 ShellTool without needs_approval (E1 hosted-kwarg) ──────────
	{"OAI-111 fires when ShellTool has no needs_approval", "OAI-111",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "ShellTool", Resolved: &models.HostedToolDef{Class: "ShellTool"}},
			},
		},
		models.RepoInventory{},
		true},
	{"OAI-111 fires on CodeInterpreterTool without needs_approval", "OAI-111",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "CodeInterpreterTool", Resolved: &models.HostedToolDef{Class: "CodeInterpreterTool"}},
			},
		},
		models.RepoInventory{},
		true},
	{"OAI-111 silent on a non-privileged hosted tool", "OAI-111",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "WebSearchTool", Resolved: &models.HostedToolDef{Class: "WebSearchTool"}},
			},
		},
		models.RepoInventory{},
		false},
	{"OAI-111 silent when ShellTool sets needs_approval=True", "OAI-111",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{
				{Class: "ShellTool", Resolved: &models.HostedToolDef{Class: "ShellTool",
					Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
						"needs_approval": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
					}}}},
			},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-110 content-fetching tool without output_guardrails ─────────────
	{"OAI-110 fires with WebSearchTool and empty output_guardrails", "OAI-110",
		models.AgentDef{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
		},
		models.RepoInventory{},
		true},
	{"OAI-110 silent when output_guardrails present", "OAI-110",
		models.AgentDef{
			SDK:            models.SDKOpenAIAgents,
			Class:          "Agent",
			Language:       models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"output_guardrails": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_output_guard"},
				}}},
			}},
		},
		models.RepoInventory{},
		false},
	{"OAI-110 silent when no content-fetching tool wired", "OAI-110",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			Language: models.LanguagePython,
			ToolRefs: []models.ToolRef{{Name: "fetch", Resolved: &models.ToolDef{Kind: models.KindOpenAITool}}},
		},
		models.RepoInventory{},
		false},

	// ─── CSDK-120 TS agent bypassPermissions ─────────────────────────────────
	{"CSDK-120 fires on bypassPermissions TS agent", "CSDK-120",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a: AgentDefinition = { description: \"x\", prompt: \"y\", permissionMode: \"bypassPermissions\" };\n"),
		models.RepoInventory{},
		true},
	{"CSDK-120 silent on TS agent without permissionMode", "CSDK-120",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a: AgentDefinition = { description: \"x\", prompt: \"y\" };\n"),
		models.RepoInventory{},
		false},
	{
		name: "CSDK-120 silent on default permissionMode", ruleID: "CSDK-120", wantFires: false,
		agent: parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a: AgentDefinition = { description: \"x\", prompt: \"y\", permissionMode: \"default\" };\n"),
	},

	// ─── CSDK-130 query() main agent granted Bash ────────────────────────────
	{"CSDK-130 fires when query main agent allows Bash", "CSDK-130",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a = query({ prompt: \"y\", options: { allowedTools: [\"Bash\", \"Read\"] } });\n"),
		models.RepoInventory{},
		true},
	{"CSDK-130 silent when query main agent has no Bash", "CSDK-130",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a = query({ prompt: \"y\", options: { allowedTools: [\"Read\", \"Grep\"] } });\n"),
		models.RepoInventory{},
		false},

	// ─── CSDK-131 query() main agent granted write/fetch built-ins ────────────
	{"CSDK-131 fires when query main agent allows Write", "CSDK-131",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a = query({ prompt: \"y\", options: { allowedTools: [\"Read\", \"Write\"] } });\n"),
		models.RepoInventory{},
		true},
	{"CSDK-131 fires when query main agent allows WebFetch", "CSDK-131",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a = query({ prompt: \"y\", options: { allowedTools: [\"WebFetch\"] } });\n"),
		models.RepoInventory{},
		true},
	{"CSDK-131 silent when query main agent has only read tools", "CSDK-131",
		parseTSAgentInline("import { query } from \"@anthropic-ai/claude-agent-sdk\";\n" +
			"const a = query({ prompt: \"y\", options: { allowedTools: [\"Read\", \"Grep\"] } });\n"),
		models.RepoInventory{},
		false},

	// ─── OAI-105 TS agent content hosted-tool without inputGuardrails ─────────
	{"OAI-105 fires on webSearchTool agent with no inputGuardrails", "OAI-105",
		parseTSOpenAIAgentInline("import { Agent, webSearchTool } from \"@openai/agents\";\n" +
			"const a = new Agent({ name: \"x\", instructions: \"y\", tools: [webSearchTool()] });\n"),
		models.RepoInventory{},
		true},
	{"OAI-105 silent when inputGuardrails present", "OAI-105",
		parseTSOpenAIAgentInline("import { Agent, webSearchTool } from \"@openai/agents\";\n" +
			"const a = new Agent({ name: \"x\", instructions: \"y\", tools: [webSearchTool()], inputGuardrails: [g] });\n"),
		models.RepoInventory{},
		false},
	{"OAI-105 silent when no content hosted tool", "OAI-105",
		parseTSOpenAIAgentInline("import { Agent } from \"@openai/agents\";\n" +
			"const a = new Agent({ name: \"x\", instructions: \"y\", tools: [] });\n"),
		models.RepoInventory{},
		false},

	// ─── ADK-109 TS LlmAgent has no description ───────────────────────────────
	{"ADK-109 fires on TS LlmAgent with no description", "ADK-109",
		parseTSADKAgentInline("import { LlmAgent } from \"@google/adk\";\n" +
			"const a = new LlmAgent({ name: \"x\", model: \"gemini-2.0-flash\" });\n"),
		models.RepoInventory{},
		true},
	{"ADK-109 silent when TS LlmAgent has description", "ADK-109",
		parseTSADKAgentInline("import { LlmAgent } from \"@google/adk\";\n" +
			"const a = new LlmAgent({ name: \"x\", model: \"gemini-2.0-flash\", description: \"routes billing questions\" });\n"),
		models.RepoInventory{},
		false},

	// ─── Pydantic AI agent rules (PYD-*) ─────────────────────────────────────
	// PYD-101: output_type omitted (free-form str) fires; output_type=str fires
	// explicitly; output_type set to a Pydantic model (Result) is validated → silent.
	{"PYD-101 fires when output_type omitted", "PYD-101",
		models.AgentDef{SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython},
		models.RepoInventory{}, true},
	{"PYD-101 fires when output_type=str", "PYD-101",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"output_type": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "str"}},
			}}},
		models.RepoInventory{}, true},
	{"PYD-101 silent when output_type is a validated model", "PYD-101",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"output_type": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "Result"}},
			}}},
		models.RepoInventory{}, false},

	{"PYD-102 fires when agent wires CodeExecutionTool", "PYD-102",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "CodeExecutionTool"}},
		},
		models.RepoInventory{}, true},
	{"PYD-102 silent without the code-execution tool", "PYD-102",
		models.AgentDef{SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython},
		models.RepoInventory{}, false},

	{"PYD-103 fires when agent wires WebFetchTool", "PYD-103",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			HostedToolRefs: []models.HostedToolRef{{Class: "WebFetchTool"}},
		},
		models.RepoInventory{}, true},
	{"PYD-103 silent without a URL-fetching native tool", "PYD-103",
		models.AgentDef{SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython},
		models.RepoInventory{}, false},

	{"PYD-105 fires when end_strategy=exhaustive", "PYD-105",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"end_strategy": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"exhaustive"`}},
			}}},
		models.RepoInventory{}, true},
	{"PYD-105 silent when end_strategy=early (default)", "PYD-105",
		models.AgentDef{
			SDK: models.SDKPydanticAI, Class: "PydanticAgent", Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"end_strategy": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"early"`}},
			}}},
		models.RepoInventory{}, false},

	// ─── Vercel AI SDK agent rules (VAI-*) ────────────────────────────────────
	// The object-record tools walk: a provider hosted-tool call
	// (anthropic.tools.bash_20250124()) becomes a HostedToolRef whose canonical
	// Class (date suffix stripped) is "anthropic.tools.bash".
	{"VAI-006 fires when agent wires anthropic bash tool", "VAI-006",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { anthropic } from \"@ai-sdk/anthropic\";\n" +
			"const r = await generateText({ model: anthropic(\"claude-sonnet-4\"), tools: { bash: anthropic.tools.bash_20250124() } });\n"),
		models.RepoInventory{},
		true},
	{"VAI-006 fires when ToolLoopAgent wires openai codeInterpreter", "VAI-006",
		parseTSVercelAgentInline("import { Experimental_Agent as Agent } from \"ai\";\n" +
			"import { openai } from \"@ai-sdk/openai\";\n" +
			"const a = new Agent({ model: openai(\"gpt-5\"), tools: { ci: openai.tools.codeInterpreter() } });\n"),
		models.RepoInventory{},
		true},
	{"VAI-006 silent when agent wires only a named user tool", "VAI-006",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { openai } from \"@ai-sdk/openai\";\n" +
			"const r = await generateText({ model: openai(\"gpt-5\"), tools: { weather: weatherTool } });\n"),
		models.RepoInventory{},
		false},

	{"VAI-007 fires when tool loop has no bound", "VAI-007",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { openai } from \"@ai-sdk/openai\";\n" +
			"const r = await generateText({ model: openai(\"gpt-5\"), tools: { weather: weatherTool } });\n"),
		models.RepoInventory{},
		true},
	{"VAI-007 silent when maxSteps set", "VAI-007",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { openai } from \"@ai-sdk/openai\";\n" +
			"const r = await generateText({ model: openai(\"gpt-5\"), maxSteps: 5, tools: { weather: weatherTool } });\n"),
		models.RepoInventory{},
		false},

	{"VAI-008 fires when toolChoice required with dangerous tool", "VAI-008",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { anthropic } from \"@ai-sdk/anthropic\";\n" +
			"const r = await generateText({ model: anthropic(\"claude-sonnet-4\"), toolChoice: \"required\", tools: { bash: anthropic.tools.bash_20250124() } });\n"),
		models.RepoInventory{},
		true},
	{"VAI-008 silent when toolChoice auto", "VAI-008",
		parseTSVercelAgentInline("import { generateText } from \"ai\";\n" +
			"import { anthropic } from \"@ai-sdk/anthropic\";\n" +
			"const r = await generateText({ model: anthropic(\"claude-sonnet-4\"), toolChoice: \"auto\", tools: { bash: anthropic.tools.bash_20250124() } });\n"),
		models.RepoInventory{},
		false},
}

func TestPolicyAgentRules(t *testing.T) {
	for _, tc := range policyAgentRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadAgentRule(t, tc.ruleID)
			if !d.Applies(tc.agent) {
				if tc.wantFires {
					t.Fatalf("rule %s does not Apply to agent %s/%s — applies_to mismatch?",
						tc.ruleID, tc.agent.SDK, tc.agent.Class)
				}
				return
			}
			fired := false
			for _, f := range d.Detect(tc.agent, tc.inv) {
				if f.RuleID == tc.ruleID {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Errorf("rule %s: fired=%v, want %v", tc.ruleID, fired, tc.wantFires)
			}
		})
	}
}

func TestPolicyRules(t *testing.T) {
	for _, tc := range policyRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadToolRule(t, tc.ruleID)
			var tool models.ToolDef
			var pf analysis.ParsedFile
			if tc.lang == models.LanguageTypeScript {
				tool, pf = parseTSTool(t, tc.src, tc.kind)
			} else {
				tool, pf = parsePy(t, tc.src, tc.kind)
			}
			if tc.toolConfig != nil {
				tool.Config = tc.toolConfig
			}
			inv := models.RepoInventory{}
			if !d.Applies(tool) {
				if tc.wantFires {
					t.Fatalf("rule %s does not Apply to a %s tool — applies_to mismatch?",
						tc.ruleID, tc.kind)
				}
				return // can't fire, satisfies wantFires=false
			}
			fired := false
			for _, f := range d.Detect(tool, pf, inv) {
				if f.RuleID == tc.ruleID {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Errorf("rule %s on snippet: fired=%v, want %v", tc.ruleID, fired, tc.wantFires)
			}
		})
	}
}

func TestPolicyRepoRules(t *testing.T) {
	for _, tc := range policyRepoRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadRepoRule(t, tc.ruleID)
			if !d.Applies(tc.profile, tc.inv) {
				if tc.wantFires {
					t.Fatalf("rule %s does not Apply — applies_to mismatch?", tc.ruleID)
				}
				return
			}
			fired := false
			for _, f := range d.Detect(tc.profile, tc.inv) {
				if f.RuleID == tc.ruleID {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Errorf("rule %s: fired=%v, want %v", tc.ruleID, fired, tc.wantFires)
			}
		})
	}
}

func TestPolicySubagentRules(t *testing.T) {
	for _, tc := range policySubagentRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadSubagentRule(t, tc.ruleID)
			if !d.Applies(tc.subagent) {
				if tc.wantFires {
					t.Fatalf("rule %s does not Apply to subagent %s — applies_to mismatch?",
						tc.ruleID, tc.subagent.Name)
				}
				return
			}
			fired := false
			for _, f := range d.Detect(tc.subagent, tc.inv) {
				if f.RuleID == tc.ruleID {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Errorf("rule %s: fired=%v, want %v", tc.ruleID, fired, tc.wantFires)
			}
		})
	}
}

// TestPolicyRules_AllRulesCovered fails unless every shipped rule has BOTH a
// fire case (wantFires=true) and a silent case (wantFires=false). One case
// alone is not enough: a fire-only case passes a predicate that always returns
// true, and a silent-only case passes one that always returns false — exactly
// the dead-predicate bugs this guard exists to catch. (Mirrors the sync
// obligation in CLAUDE.md: "fire + silent cases".)
func TestPolicyRules_AllRulesCovered(t *testing.T) {
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	hasFire := map[string]bool{}
	hasSilent := map[string]bool{}
	record := func(id string, fires bool) {
		if fires {
			hasFire[id] = true
		} else {
			hasSilent[id] = true
		}
	}
	for _, tc := range policyRuleCases {
		record(tc.ruleID, tc.wantFires)
	}
	for _, tc := range policyAgentRuleCases {
		record(tc.ruleID, tc.wantFires)
	}
	for _, tc := range policyRepoRuleCases {
		record(tc.ruleID, tc.wantFires)
	}
	for _, tc := range policySubagentRuleCases {
		record(tc.ruleID, tc.wantFires)
	}
	var missingAny, missingFire, missingSilent []string
	for _, p := range policies {
		for _, r := range p.Rules {
			f, s := hasFire[r.ID], hasSilent[r.ID]
			switch {
			case !f && !s:
				missingAny = append(missingAny, r.ID)
			case !f:
				missingFire = append(missingFire, r.ID)
			case !s:
				missingSilent = append(missingSilent, r.ID)
			}
		}
	}
	if len(missingAny) > 0 {
		t.Errorf("rules with no policy_test coverage at all: %v", missingAny)
	}
	if len(missingFire) > 0 {
		t.Errorf("rules missing a FIRE case (wantFires=true): %v", missingFire)
	}
	if len(missingSilent) > 0 {
		t.Errorf("rules missing a SILENT case (wantFires=false): %v", missingSilent)
	}
}

// TestFixtureAgentsHaveLanguage guards the language-gate contract added in
// Task 5: every AgentDef fixture used by the policy-rule tests must carry
// an explicit Language so the gate doesn't silently reject Python agents.
func TestFixtureAgentsHaveLanguage(t *testing.T) {
	for _, c := range policyAgentRuleCases {
		// Check fire case
		if c.agent.Language == "" {
			t.Errorf("rule %s fire case has AgentDef with empty Language", c.ruleID)
		}
		// Check silent case (in RepoInventory)
		for _, a := range c.inv.Agents {
			if a.Language == "" {
				t.Errorf("rule %s silent case has AgentDef with empty Language", c.ruleID)
			}
		}
	}
}
