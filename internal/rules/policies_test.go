package rules_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	src        string            // Python snippet
	toolConfig map[string]string // optional Config override (for decorator-kwarg rules)
	wantFires  bool              // expected: rule fires for this snippet
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
	// ─── CSDK-001 missing docstring ─────────────────────────────────────────
	{"CSDK-001 fires on missing docstring", "CSDK-001", models.KindClaudeSDKTool, `
def fetch_data(x: str) -> dict:
    return {}
`, nil, true},
	{"CSDK-001 silent with docstring", "CSDK-001", models.KindClaudeSDKTool, `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`, nil, false},

	// ─── CSDK-002 untyped params ────────────────────────────────────────────
	{"CSDK-002 fires on untyped params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data(x, y):
    """Does something."""
    return {}
`, nil, true},
	{"CSDK-002 silent with typed params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`, nil, false},
	{"CSDK-002 silent with no params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data() -> dict:
    """No params, no problem."""
    return {}
`, nil, false},

	// ─── CSDK-003 network without timeout ───────────────────────────────────
	{"CSDK-003 fires without timeout", "CSDK-003", models.KindClaudeSDKTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id).json()
`, nil, true},
	{"CSDK-003 silent with timeout", "CSDK-003", models.KindClaudeSDKTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id, timeout=10).json()
`, nil, false},
	{"CSDK-003 silent on non-HTTP call", "CSDK-003", models.KindClaudeSDKTool, `
def get_data(cache_key: str) -> dict:
    """Read from cache."""
    return cache.fetch(cache_key)
`, nil, false},
	{"CSDK-003 fires on session-alias get without timeout", "CSDK-003", models.KindClaudeSDKTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    s = requests.Session()
    return s.get("https://api.example.com/invoice/" + id).json()
`, nil, true},

	// ─── CSDK-004 unsafe path ───────────────────────────────────────────────
	{"CSDK-004 fires on path in open()", "CSDK-004", models.KindClaudeSDKTool, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, nil, true},
	{"CSDK-004 silent with .resolve()", "CSDK-004", models.KindClaudeSDKTool, `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`, nil, false},
	{"CSDK-004 silent on non-pathish param", "CSDK-004", models.KindClaudeSDKTool, `
def get_editor(editor_id: str) -> dict:
    """Get editor config."""
    return {"id": editor_id}
`, nil, false},

	// ─── CSDK-005 raw exceptions ────────────────────────────────────────────
	{"CSDK-005 fires on raise without try", "CSDK-005", models.KindClaudeSDKTool, `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`, nil, true},
	{"CSDK-005 silent with try/except", "CSDK-005", models.KindClaudeSDKTool, `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`, nil, false},

	// ─── CSDK-006 idempotency ───────────────────────────────────────────────
	{"CSDK-006 fires on mutating tool without key", "CSDK-006", models.KindClaudeSDKTool, `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, true},
	{"CSDK-006 silent with idempotency key", "CSDK-006", models.KindClaudeSDKTool, `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, false},
	{"CSDK-006 silent on non-mutating name", "CSDK-006", models.KindClaudeSDKTool, `
def get_order(order_id: str) -> dict:
    """Fetch an order."""
    return {"id": order_id}
`, nil, false},

	// ─── CSDK-007 ambiguous name ────────────────────────────────────────────
	{"CSDK-007 fires on ambiguous name", "CSDK-007", models.KindClaudeSDKTool, `
def process(data: dict) -> dict:
    """Process data."""
    return data
`, nil, true},
	{"CSDK-007 silent on descriptive name", "CSDK-007", models.KindClaudeSDKTool, `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`, nil, false},

	// ─── OAI-001 missing docstring ───────────────────────────────────────────
	{"OAI-001 fires on missing docstring", "OAI-001", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    return {}
`, nil, true},
	{"OAI-001 silent with docstring", "OAI-001", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`, nil, false},

	// ─── OAI-002 untyped params ─────────────────────────────────────────────
	{"OAI-002 fires on untyped params", "OAI-002", models.KindOpenAITool, `
def fetch_data(x, y):
    """Does something."""
    return {}
`, nil, true},
	{"OAI-002 silent with typed params", "OAI-002", models.KindOpenAITool, `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`, nil, false},

	// ─── OAI-003 strict_mode=False ──────────────────────────────────────────
	{"OAI-003 fires when strict_mode=False in config", "OAI-003", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`, map[string]string{"strict_mode": "False"}, true},
	{"OAI-003 silent when strict_mode not set", "OAI-003", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`, nil, false},

	// ─── OAI-004 no failure_error_function ──────────────────────────────────
	{"OAI-004 fires when failure_error_function absent", "OAI-004", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`, nil, true},
	{"OAI-004 silent when failure_error_function present", "OAI-004", models.KindOpenAITool, `
def fetch_data(x: str) -> dict:
    """Fetch data."""
    return {}
`, map[string]string{"failure_error_function": "handle_error"}, false},

	// ─── OAI-005 network without timeout ────────────────────────────────────
	{"OAI-005 fires without timeout", "OAI-005", models.KindOpenAITool, `
import requests
def get_data(id: str) -> dict:
    """Get data."""
    return requests.get("https://api.example.com/" + id).json()
`, nil, true},
	{"OAI-005 silent with timeout", "OAI-005", models.KindOpenAITool, `
import requests
def get_data(id: str) -> dict:
    """Get data."""
    return requests.get("https://api.example.com/" + id, timeout=10).json()
`, nil, false},

	// ─── OAI-006 unsafe path ────────────────────────────────────────────────
	{"OAI-006 fires on path in open()", "OAI-006", models.KindOpenAITool, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, nil, true},
	{"OAI-006 silent with .resolve()", "OAI-006", models.KindOpenAITool, `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`, nil, false},

	// ─── ADK-001 missing docstring on FunctionTool wrap ──────────────────────
	{"ADK-001 fires on missing docstring", "ADK-001", models.KindADKFunctionTool, `
def get_weather(city: str) -> str:
    return "sunny"
`, nil, true},
	{"ADK-001 silent with docstring", "ADK-001", models.KindADKFunctionTool, `
def get_weather(city: str) -> str:
    """Look up the weather for a city."""
    return "sunny"
`, nil, false},

	// ─── ADK-002 untyped params on FunctionTool wrap ─────────────────────────
	{"ADK-002 fires on untyped params", "ADK-002", models.KindADKFunctionTool, `
def get_weather(city):
    """Look up the weather."""
    return "sunny"
`, nil, true},
	{"ADK-002 silent on typed params", "ADK-002", models.KindADKFunctionTool, `
def get_weather(city: str) -> str:
    """Look up the weather."""
    return "sunny"
`, nil, false},

	// ─── ADK-003 network call without timeout ────────────────────────────────
	{"ADK-003 fires on requests.get without timeout", "ADK-003", models.KindADKFunctionTool, `
import requests

def get_weather(city: str) -> str:
    """Look up the weather."""
    return requests.get("https://api.example.com/w/" + city).text
`, nil, true},
	{"ADK-003 silent with timeout", "ADK-003", models.KindADKFunctionTool, `
import requests

def get_weather(city: str) -> str:
    """Look up the weather."""
    return requests.get("https://api.example.com/w/" + city, timeout=10).text
`, nil, false},

	// ─── alias + None coverage (OAI-005, ADK-003) ────────────────────────────
	{"OAI-005 fires on session-alias get without timeout", "OAI-005", models.KindOpenAITool, `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    s = requests.Session()
    return s.get(url).text
`, nil, true},
	{"OAI-005 fires on timeout=None", "OAI-005", models.KindOpenAITool, `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    return requests.get(url, timeout=None).text
`, nil, true},
	{"ADK-003 fires on session-alias get without timeout", "ADK-003", models.KindADKFunctionTool, `
import requests
def fetch(url: str) -> str:
    """Fetch."""
    s = requests.Session()
    return s.get(url).text
`, nil, true},

	// ─── OAI-007 ambiguous name ──────────────────────────────────────────────
	{"OAI-007 fires on ambiguous name", "OAI-007", models.KindOpenAITool, `
def process(data: dict) -> dict:
    """Process data."""
    return data
`, nil, true},
	{"OAI-007 silent on descriptive name", "OAI-007", models.KindOpenAITool, `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`, nil, false},

	// ─── OAI-008 raw exceptions ──────────────────────────────────────────────
	{"OAI-008 fires on raise without try", "OAI-008", models.KindOpenAITool, `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`, nil, true},
	{"OAI-008 silent with try/except", "OAI-008", models.KindOpenAITool, `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`, nil, false},

	// ─── OAI-009 idempotency ─────────────────────────────────────────────────
	{"OAI-009 fires on mutating tool without key", "OAI-009", models.KindOpenAITool, `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, true},
	{"OAI-009 silent with idempotency key", "OAI-009", models.KindOpenAITool, `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, false},

	// ─── OAI-010 print to stdout ─────────────────────────────────────────────
	{"OAI-010 fires on print()", "OAI-010", models.KindOpenAITool, `
def fetch(x: str) -> dict:
    """Fetch."""
    print("debug", x)
    return {}
`, nil, true},
	{"OAI-010 silent without print", "OAI-010", models.KindOpenAITool, `
def fetch(x: str) -> dict:
    """Fetch."""
    return {}
`, nil, false},

	// ─── OAI-011 urllib without timeout ──────────────────────────────────────
	{"OAI-011 fires on urlopen without timeout", "OAI-011", models.KindOpenAITool, `
import urllib.request
def fetch(url: str) -> bytes:
    """Fetch."""
    return urllib.request.urlopen(url).read()
`, nil, true},
	{"OAI-011 silent with timeout", "OAI-011", models.KindOpenAITool, `
import urllib.request
def fetch(url: str) -> bytes:
    """Fetch."""
    return urllib.request.urlopen(url, timeout=10).read()
`, nil, false},

	// ─── OAI-012 subprocess spawn ────────────────────────────────────────────
	{"OAI-012 fires on subprocess.run", "OAI-012", models.KindOpenAITool, `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    return subprocess.run([cmd], capture_output=True).stdout.decode()
`, nil, true},
	{"OAI-012 silent without subprocess", "OAI-012", models.KindOpenAITool, `
def run(cmd: str) -> str:
    """Run."""
    return cmd.upper()
`, nil, false},

	// ─── OAI-013 dynamic code execution ──────────────────────────────────────
	{"OAI-013 fires on eval", "OAI-013", models.KindOpenAITool, `
def calc(expr: str):
    """Calc."""
    return eval(expr)
`, nil, true},
	{"OAI-013 silent without eval/exec/compile", "OAI-013", models.KindOpenAITool, `
def calc(expr: str) -> int:
    """Calc."""
    return int(expr) + 1
`, nil, false},

	// ─── ADK-004 unsafe path ─────────────────────────────────────────────────
	{"ADK-004 fires on path in open()", "ADK-004", models.KindADKFunctionTool, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, nil, true},
	{"ADK-004 silent with .resolve()", "ADK-004", models.KindADKFunctionTool, `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`, nil, false},

	// ─── ADK-005 raw exceptions ──────────────────────────────────────────────
	{"ADK-005 fires on raise without try", "ADK-005", models.KindADKFunctionTool, `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`, nil, true},
	{"ADK-005 silent with try/except", "ADK-005", models.KindADKFunctionTool, `
def process(x: str) -> dict:
    """Process x."""
    try:
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`, nil, false},

	// ─── ADK-006 idempotency ─────────────────────────────────────────────────
	{"ADK-006 fires on mutating tool without key", "ADK-006", models.KindADKFunctionTool, `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, true},
	{"ADK-006 silent with idempotency key", "ADK-006", models.KindADKFunctionTool, `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, nil, false},

	// ─── ADK-007 ambiguous name ──────────────────────────────────────────────
	{"ADK-007 fires on ambiguous name", "ADK-007", models.KindADKFunctionTool, `
def handle(data: dict) -> dict:
    """Handle data."""
    return data
`, nil, true},
	{"ADK-007 silent on descriptive name", "ADK-007", models.KindADKFunctionTool, `
def fetch_order(order_id: str) -> dict:
    """Fetch an order."""
    return {}
`, nil, false},

	// ─── ADK-008 BashTool metacharacter blocking ─────────────────────────────
	{"ADK-008 fires when block_shell_metacharacters not set", "ADK-008", models.KindADKFunctionTool, `
def BashTool(command: str) -> str:
    """Run a shell command."""
    return ""
`, nil, true},
	{"ADK-008 silent when block_shell_metacharacters=True", "ADK-008", models.KindADKFunctionTool, `
def BashTool(command: str) -> str:
    """Run a shell command."""
    return ""
`, map[string]string{"block_shell_metacharacters": "True"}, false},

	// ─── OAI-010 FP-safety: structured has_print_call ignores pprint ──────────
	{"OAI-010 silent on pprint (not the print builtin)", "OAI-010", models.KindOpenAITool, `
from pprint import pprint
def fetch(x: dict) -> dict:
    """Fetch."""
    pprint(x)
    return x
`, nil, false},

	// ─── OAI-013 FP-safety: structured has_code_exec_call ignores re.compile ──
	{"OAI-013 silent on re.compile (not the compile builtin)", "OAI-013", models.KindOpenAITool, `
import re
def build(pattern: str):
    """Build."""
    return re.compile(pattern)
`, nil, false},

	// ─── mcp_tool scope restored on CSDK tool-hygiene rules ──────────────────
	// CSDK-001/002/003/007 apply to [claude_sdk_tool, mcp_tool]; these cases
	// exercise the mcp_tool half that the fixture had drifted to drop.
	{"CSDK-001 fires on MCP tool missing docstring", "CSDK-001", models.KindMCPTool, `
def fetch_data(x: str) -> dict:
    return {}
`, nil, true},
	{"CSDK-003 fires on MCP tool network call without timeout", "CSDK-003", models.KindMCPTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id).json()
`, nil, true},
}

// policyRepoRuleCases covers repo-scoped rules.
var policyRepoRuleCases = []policyRepoCase{
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
}

// policySubagentRuleCases covers subagent-scoped rules.
var policySubagentRuleCases = []policySubagentCase{
	{"CSDK-110 fires when subagent grants Bash", "CSDK-110",
		models.SubagentDef{Name: "inbox-searcher", Location: models.Location{FilePath: ".claude/agents/inbox-searcher.md"},
			Tools: []string{"Read", "Bash", "Grep"}}, models.RepoInventory{}, true},
	{"CSDK-110 silent when no Bash", "CSDK-110",
		models.SubagentDef{Name: "reader", Location: models.Location{FilePath: ".claude/agents/reader.md"},
			Tools: []string{"Read", "Grep"}}, models.RepoInventory{}, false},
}

// policyAgentRuleCases covers agent-scoped rules.
var policyAgentRuleCases = []policyAgentCase{
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
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Location: models.Location{FilePath: "main.py"},
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
			SDK:      models.SDKGoogleADK,
			Class:    "LlmAgent",
			Language: models.LanguagePython,
			Location: models.Location{FilePath: "main.py"},
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
	{"CSDK-102 fires when AgentDefinition grants WebSearch", "CSDK-102",
		models.AgentDef{
			SDK:            models.SDKClaudeAgentSDK,
			Class:          "AgentDefinition",
			Language:       models.LanguagePython,
			Name:           "researcher",
			HostedToolRefs: []models.HostedToolRef{{Class: "WebSearch"}},
		},
		models.RepoInventory{},
		true},
	{"CSDK-102 silent when no WebSearch granted", "CSDK-102",
		models.AgentDef{
			SDK:            models.SDKClaudeAgentSDK,
			Class:          "AgentDefinition",
			Language:       models.LanguagePython,
			Name:           "writer",
			HostedToolRefs: []models.HostedToolRef{{Class: "Read"}},
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
	{"ADK-105 fires with google_search and no before_tool_callback", "ADK-105",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Language:       models.LanguagePython,
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "google_search"}},
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
			HostedToolRefs: []models.HostedToolRef{{Class: "google_search"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "my_guard"}},
			}},
		},
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
			tool, pf := parsePy(t, tc.src, tc.kind)
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

// TestPolicyRules_AllRulesCovered fails if a shipped rule has no test case.
func TestPolicyRules_AllRulesCovered(t *testing.T) {
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	covered := map[string]bool{}
	for _, tc := range policyRuleCases {
		covered[tc.ruleID] = true
	}
	for _, tc := range policyAgentRuleCases {
		covered[tc.ruleID] = true
	}
	for _, tc := range policyRepoRuleCases {
		covered[tc.ruleID] = true
	}
	for _, tc := range policySubagentRuleCases {
		covered[tc.ruleID] = true
	}
	var missing []string
	for _, p := range policies {
		for _, r := range p.Rules {
			if !covered[r.ID] {
				missing = append(missing, r.ID)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("rules without policy_test coverage: %v", missing)
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
