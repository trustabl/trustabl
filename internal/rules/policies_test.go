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
}

// policyRepoRuleCases covers repo-scoped rules.
var policyRepoRuleCases = []policyRepoCase{
	// ─── OAI-201 default tracing (repo-scoped) ───────────────────────────────
	{"OAI-201 fires when using default tracing", "OAI-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKOpenAIAgents},
			UsesDefaultTracing: true,
		},
		true},
	{"OAI-201 silent when custom tracing configured", "OAI-201",
		models.RepoProfile{},
		models.RepoInventory{
			SDKsDetected:       []models.SDK{models.SDKOpenAIAgents},
			UsesDefaultTracing: false,
		},
		false},
}

// policyAgentRuleCases covers agent-scoped rules.
var policyAgentRuleCases = []policyAgentCase{
	// ─── OAI-101 no input_guardrails + shell tools ────────────────────────────
	{"OAI-101 fires when no guardrails and has shell tool", "OAI-101",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			ToolRefs: []models.ToolRef{{Name: "run_cmd", Resolved: &models.ToolDef{Kind: models.KindShellInvocation}}},
		},
		models.RepoInventory{},
		true},
	{"OAI-101 silent when input_guardrails present", "OAI-101",
		models.AgentDef{
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
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
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"tool_use_behavior": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"stop_on_first_tool"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"OAI-102 silent on default behavior", "OAI-102",
		models.AgentDef{
			SDK:    models.SDKOpenAIAgents,
			Class:  "Agent",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-103 tool_choice=required + reset_tool_choice=False ──────────────
	{"OAI-103 fires on loop pattern", "OAI-103",
		models.AgentDef{
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
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
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
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
			ToolRefs: []models.ToolRef{{Name: "run_cmd", Resolved: &models.ToolDef{Kind: models.KindShellInvocation}}},
		},
		models.RepoInventory{},
		true},
	{"OAI-104 silent on Agent with no shell tools", "OAI-104",
		models.AgentDef{
			SDK:      models.SDKOpenAIAgents,
			Class:    "Agent",
			ToolRefs: []models.ToolRef{{Name: "fetch", Resolved: &models.ToolDef{Kind: models.KindOpenAITool}}},
		},
		models.RepoInventory{},
		false},

	// ─── OAI-105 mcp_servers + no input_guardrails ───────────────────────────
	{"OAI-105 fires with mcp_servers and no guardrails", "OAI-105",
		models.AgentDef{
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"mcp_servers": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "my_mcp"},
				}}},
			}},
		},
		models.RepoInventory{},
		true},
	{"OAI-105 silent when input_guardrails also present", "OAI-105",
		models.AgentDef{
			SDK:   models.SDKOpenAIAgents,
			Class: "Agent",
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
			SDK:   models.SDKClaudeAgentSDK,
			Class: "AgentDefinition",
			Name:  "data-analyst",
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
			SDK:   models.SDKClaudeAgentSDK,
			Class: "AgentDefinition",
			Name:  "researcher",
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
			SDK:   models.SDKGoogleADK,
			Class: "LlmAgent",
			Name:  "child",
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"child"`}},
			}},
		},
		models.RepoInventory{},
		true},
	{"ADK-101 silent when LlmAgent has description", "ADK-101",
		models.AgentDef{
			SDK:   models.SDKGoogleADK,
			Class: "LlmAgent",
			Name:  "child",
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
			Name:           "child",
			FilePath:       "main.py",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
		},
		models.RepoInventory{Agents: []models.AgentDef{
			{
				SDK:      models.SDKGoogleADK,
				Class:    "SequentialAgent",
				Name:     "parent",
				FilePath: "main.py",
				HandoffRefs: []models.AgentRef{
					{Name: "child", Resolved: &models.AgentDef{Name: "child", FilePath: "main.py"}},
				},
			},
		}},
		true},
	{"ADK-103 silent on root agent (not a sub-agent of any)", "ADK-103",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Name:           "root",
			FilePath:       "main.py",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
		},
		models.RepoInventory{Agents: []models.AgentDef{
			{
				SDK:      models.SDKGoogleADK,
				Class:    "LlmAgent",
				Name:     "sibling",
				FilePath: "main.py",
			},
		}},
		false},

	// ─── ADK-102 before_tool_callback=None counts as missing ─────────────────
	{"ADK-102 fires when before_tool_callback is None", "ADK-102",
		models.AgentDef{
			SDK:            models.SDKGoogleADK,
			Class:          "LlmAgent",
			Name:           "root",
			HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}},
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"name":                 {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"root"`}},
				"before_tool_callback": {Value: &models.Expr{Kind: models.ExprLiteralNone, Text: "None"}},
			}},
		},
		models.RepoInventory{},
		true},
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
