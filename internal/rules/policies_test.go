package rules_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/detectors"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// loadToolRule fetches a tool-scoped rule from shipped policies as a ToolDetector.
func loadToolRule(t *testing.T, ruleID string) detectors.ToolDetector {
	t.Helper()
	policies, err := rules.Load(rules.DefaultFS())
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
	policies, err := rules.Load(rules.DefaultFS())
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
	policies, err := rules.Load(rules.DefaultFS())
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
	name      string          // test subname
	ruleID    string          // YAML rule ID under test
	kind      models.ToolKind // ToolKind for the synthetic tool
	src       string          // Python snippet
	wantFires bool            // expected: rule fires for this snippet
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
`, true},
	{"CSDK-001 silent with docstring", "CSDK-001", models.KindClaudeSDKTool, `
def fetch_data(x: str) -> dict:
    """Fetch some data."""
    return {}
`, false},

	// ─── CSDK-002 untyped params ────────────────────────────────────────────
	{"CSDK-002 fires on untyped params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data(x, y):
    """Does something."""
    return {}
`, true},
	{"CSDK-002 silent with typed params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data(x: str, y: int) -> dict:
    """Does something."""
    return {}
`, false},
	{"CSDK-002 silent with no params", "CSDK-002", models.KindClaudeSDKTool, `
def fetch_data() -> dict:
    """No params, no problem."""
    return {}
`, false},

	// ─── CSDK-003 network without timeout ───────────────────────────────────
	{"CSDK-003 fires without timeout", "CSDK-003", models.KindClaudeSDKTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id).json()
`, true},
	{"CSDK-003 silent with timeout", "CSDK-003", models.KindClaudeSDKTool, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/invoice/" + id, timeout=10).json()
`, false},
	{"CSDK-003 silent on non-HTTP call", "CSDK-003", models.KindClaudeSDKTool, `
def get_data(cache_key: str) -> dict:
    """Read from cache."""
    return cache.fetch(cache_key)
`, false},

	// ─── CSDK-004 unsafe path ───────────────────────────────────────────────
	{"CSDK-004 fires on path in open()", "CSDK-004", models.KindClaudeSDKTool, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, true},
	{"CSDK-004 silent with .resolve()", "CSDK-004", models.KindClaudeSDKTool, `
from pathlib import Path
def read_file(file_path: str) -> str:
    """Read a file."""
    p = Path(file_path).resolve()
    with open(p, "r") as f:
        return f.read()
`, false},
	{"CSDK-004 silent on non-pathish param", "CSDK-004", models.KindClaudeSDKTool, `
def get_editor(editor_id: str) -> dict:
    """Get editor config."""
    return {"id": editor_id}
`, false},

	// ─── CSDK-005 raw exceptions ────────────────────────────────────────────
	{"CSDK-005 fires on raise without try", "CSDK-005", models.KindClaudeSDKTool, `
def process(x: str) -> dict:
    """Process x."""
    if not x:
        raise ValueError("empty input")
    return {"x": x}
`, true},
	{"CSDK-005 silent with try/except", "CSDK-005", models.KindClaudeSDKTool, `
def process(x: str) -> dict:
    """Process x."""
    try:
        if not x:
            raise ValueError("empty")
        return {"x": x}
    except ValueError as e:
        return {"error": str(e)}
`, false},

	// ─── CSDK-006 idempotency ───────────────────────────────────────────────
	{"CSDK-006 fires on mutating tool without key", "CSDK-006", models.KindClaudeSDKTool, `
def create_order(customer_id: str, amount: float) -> dict:
    """Create an order."""
    return {"ok": True}
`, true},
	{"CSDK-006 silent with idempotency key", "CSDK-006", models.KindClaudeSDKTool, `
def create_order(customer_id: str, amount: float, idempotency_key: str) -> dict:
    """Create an order."""
    return {"ok": True}
`, false},
	{"CSDK-006 silent on non-mutating name", "CSDK-006", models.KindClaudeSDKTool, `
def get_order(order_id: str) -> dict:
    """Fetch an order."""
    return {"id": order_id}
`, false},

	// ─── CSDK-007 ambiguous name ────────────────────────────────────────────
	{"CSDK-007 fires on ambiguous name", "CSDK-007", models.KindClaudeSDKTool, `
def process(data: dict) -> dict:
    """Process data."""
    return data
`, true},
	{"CSDK-007 silent on descriptive name", "CSDK-007", models.KindClaudeSDKTool, `
def summarize_invoice(invoice_id: str) -> dict:
    """Summarize an invoice."""
    return {}
`, false},

	// ─── OSH-001 shell=True ─────────────────────────────────────────────────
	{"OSH-001 fires on shell=True", "OSH-001", models.KindShellInvocation, `
import subprocess
def run_report(name: str) -> str:
    """Run report tool."""
    subprocess.run(f"report-tool {name}", shell=True)
    return "done"
`, true},
	{"OSH-001 silent on list-form call", "OSH-001", models.KindShellInvocation, `
import subprocess
def run_report(name: str) -> str:
    """Run report tool."""
    subprocess.run(["report-tool", name])
    return "done"
`, false},

	// ─── OSH-002 no allowlist ───────────────────────────────────────────────
	{"OSH-002 fires without allowlist", "OSH-002", models.KindShellInvocation, `
import subprocess
def run_cmd(cmd: str) -> str:
    """Run a command."""
    subprocess.run([cmd])
    return "done"
`, true},
	{"OSH-002 silent with ALLOWED_COMMANDS", "OSH-002", models.KindShellInvocation, `
import subprocess
ALLOWED_COMMANDS = ["git", "python3"]
def run_cmd(cmd: str) -> str:
    """Run an allowed command."""
    assert cmd in ALLOWED_COMMANDS
    subprocess.run([cmd])
    return "done"
`, false},

	// ─── OSH-003 unrestricted fs write ──────────────────────────────────────
	{"OSH-003 fires on open(..., 'w')", "OSH-003", models.KindShellInvocation, `
def write_output(name: str) -> str:
    """Write output."""
    with open(f"/tmp/{name}.txt", "w") as f:
        f.write("data")
    return "done"
`, true},
	{"OSH-003 silent on read-only open", "OSH-003", models.KindShellInvocation, `
def read_output(name: str) -> str:
    """Read output."""
    with open(f"/tmp/{name}.txt", "r") as f:
        return f.read()
`, false},

	// ─── OSH-005 broad network egress ───────────────────────────────────────
	{"OSH-005 fires on dynamic URL", "OSH-005", models.KindClaudeSDKTool, `
import requests
def fetch_resource(url: str) -> dict:
    """Fetch from a dynamic URL."""
    return requests.get(url).json()
`, true},
	{"OSH-005 silent on literal URL", "OSH-005", models.KindClaudeSDKTool, `
import requests
def fetch_resource() -> dict:
    """Fetch from a known endpoint."""
    return requests.get("https://api.example.com/data").json()
`, false},
}

// policyRepoRuleCases covers repo-scoped rules.
var policyRepoRuleCases = []policyRepoCase{
	// ─── OSH-004 no resource limits (repo-scoped) ────────────────────────────
	{"OSH-004 fires when openshell artifact present", "OSH-004",
		models.RepoProfile{Manifest: models.ScanManifest{HasOpenShellArtifact: true}},
		models.RepoInventory{SDKsDetected: []models.SDK{}, Manifest: models.ScanManifest{HasOpenShellArtifact: true}},
		true},
	{"OSH-004 silent when no openshell artifact", "OSH-004",
		models.RepoProfile{},
		models.RepoInventory{},
		false},
}

func TestPolicyRules(t *testing.T) {
	for _, tc := range policyRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadToolRule(t, tc.ruleID)
			tool, pf := parsePy(t, tc.src, tc.kind)
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
	policies, err := rules.Load(rules.DefaultFS())
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	covered := map[string]bool{}
	for _, tc := range policyRuleCases {
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
