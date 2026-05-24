package rules_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// parsePy parses a Python snippet and returns ParsedFile + ToolDef for the
// first function_definition found. kind is the ToolKind to assign.
func parsePy(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	b := []byte(src)
	tree, err := astutil.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "test.py", Source: b, Tree: tree}
	fns := astutil.FindAll(tree.RootNode(), "function_definition", "decorated_definition")
	if len(fns) == 0 {
		t.Fatal("no function found")
	}
	fn := astutil.FunctionDef(fns[0])
	if fn == nil {
		fn = fns[0]
	}
	name := astutil.FunctionName(fn, b)
	doc := astutil.FunctionDocstring(fn, b)
	params := astutil.FunctionParams(fn, b)
	filtered := params[:0]
	for _, p := range params {
		if p != "self" && p != "cls" {
			filtered = append(filtered, p)
		}
	}
	tool := models.ToolDef{
		Name:           name,
		Kind:           kind,
		Language:       models.LanguagePython,
		FilePath:       pf.RelPath,
		Line:           astutil.NodeLine(fn),
		EndLine:        astutil.NodeEndLine(fn),
		Description:    doc,
		HasTypedParams: astutil.FunctionHasTypedParams(fn),
		ParamNames:     filtered,
		Facts:          map[string]string{},
	}
	return tool, pf
}

// ─── has_docstring ────────────────────────────────────────────────────────────

func TestPred_HasDocstring_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    """Does stuff."""
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasDocstring(tool) {
		t.Error("expected HasDocstring true")
	}
}

func TestPred_HasDocstring_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasDocstring(tool) {
		t.Error("expected HasDocstring false")
	}
}

// ─── has_params ───────────────────────────────────────────────────────────────

func TestPred_HasParams_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasParams(tool) {
		t.Error("expected HasParams true")
	}
}

func TestPred_HasParams_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo() -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasParams(tool) {
		t.Error("expected HasParams false")
	}
}

// ─── has_typed_params ─────────────────────────────────────────────────────────

func TestPred_HasTypedParams_True(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x: str) -> dict:
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasTypedParams(tool) {
		t.Error("expected HasTypedParams true")
	}
}

func TestPred_HasTypedParams_False(t *testing.T) {
	tool, _ := parsePy(t, `
def foo(x, y):
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasTypedParams(tool) {
		t.Error("expected HasTypedParams false")
	}
}

// ─── has_raise ────────────────────────────────────────────────────────────────

func TestPred_HasRaise_True(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    if not x:
        raise ValueError("empty")
    return {}
`, models.KindClaudeSDKTool)
	if !rules.PredHasRaise(tool, pf) {
		t.Error("expected HasRaise true")
	}
}

func TestPred_HasRaise_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasRaise(tool, pf) {
		t.Error("expected HasRaise false")
	}
}

// ─── has_try_except ───────────────────────────────────────────────────────────

func TestPred_HasTryExcept_True(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    try:
        return {"x": x}
    except Exception as e:
        return {"error": str(e)}
`, models.KindClaudeSDKTool)
	if !rules.PredHasTryExcept(tool, pf) {
		t.Error("expected HasTryExcept true")
	}
}

func TestPred_HasTryExcept_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> dict:
    """Foo."""
    return {}
`, models.KindClaudeSDKTool)
	if rules.PredHasTryExcept(tool, pf) {
		t.Error("expected HasTryExcept false")
	}
}

// ─── name_in ──────────────────────────────────────────────────────────────────

func TestPred_NameIn_Hit(t *testing.T) {
	tool := models.ToolDef{Name: "process"}
	if !rules.PredNameIn([]string{"process", "handle"}, tool) {
		t.Error("expected NameIn hit")
	}
}

func TestPred_NameIn_Miss(t *testing.T) {
	tool := models.ToolDef{Name: "summarize_invoice"}
	if rules.PredNameIn([]string{"process", "handle"}, tool) {
		t.Error("expected NameIn miss")
	}
}

// ─── name_has_prefix ──────────────────────────────────────────────────────────

func TestPred_NameHasPrefix_Hit(t *testing.T) {
	tool := models.ToolDef{Name: "create_order"}
	if !rules.PredNameHasPrefix([]string{"create_", "send_"}, tool) {
		t.Error("expected NameHasPrefix hit")
	}
}

func TestPred_NameHasPrefix_Miss(t *testing.T) {
	tool := models.ToolDef{Name: "get_order"}
	if rules.PredNameHasPrefix([]string{"create_", "send_"}, tool) {
		t.Error("expected NameHasPrefix miss")
	}
}

// ─── param_name_matches ───────────────────────────────────────────────────────

func TestPred_ParamNameMatches_ExactHit(t *testing.T) {
	tool := models.ToolDef{ParamNames: []string{"path"}}
	expr := rules.ParamNameMatchExpr{Exact: []string{"path", "file"}}
	if !rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches hit on exact 'path'")
	}
}

func TestPred_ParamNameMatches_SuffixHit(t *testing.T) {
	tool := models.ToolDef{ParamNames: []string{"output_path"}}
	expr := rules.ParamNameMatchExpr{Suffixes: []string{"_path", "_file"}}
	if !rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches hit on suffix '_path'")
	}
}

func TestPred_ParamNameMatches_Miss_SubstringOnly(t *testing.T) {
	// "editor_id" contains "dir" but should NOT match suffix "_dir"
	tool := models.ToolDef{ParamNames: []string{"editor_id"}}
	expr := rules.ParamNameMatchExpr{Suffixes: []string{"_dir"}}
	if rules.PredParamNameMatches(expr, tool) {
		t.Error("expected ParamNameMatches miss for 'editor_id' vs '_dir' suffix")
	}
}

// ─── has_body_text ────────────────────────────────────────────────────────────

func TestPred_HasBodyText_Hit(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(p: str) -> str:
    """Foo."""
    return Path(p).resolve()
`, models.KindClaudeSDKTool)
	if !rules.PredHasBodyText([]string{".resolve(", "realpath("}, tool, pf) {
		t.Error("expected HasBodyText hit")
	}
}

func TestPred_HasBodyText_Miss(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(p: str) -> str:
    """Foo."""
    return open(p).read()
`, models.KindClaudeSDKTool)
	if rules.PredHasBodyText([]string{".resolve(", "realpath("}, tool, pf) {
		t.Error("expected HasBodyText miss")
	}
}

// ─── has_shell_call ───────────────────────────────────────────────────────────

func TestPred_HasShellCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(cmd: str) -> str:
    """Run."""
    subprocess.run([cmd])
    return "done"
`, models.KindShellInvocation)
	if !rules.PredHasShellCall(tool, pf) {
		t.Error("expected HasShellCall true")
	}
}

func TestPred_HasShellCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
def foo(x: str) -> str:
    """Foo."""
    return x
`, models.KindClaudeSDKTool)
	if rules.PredHasShellCall(tool, pf) {
		t.Error("expected HasShellCall false")
	}
}

// ─── has_write_call ───────────────────────────────────────────────────────────

func TestPred_HasWriteCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
def write(name: str) -> str:
    """Write."""
    with open(f"/tmp/{name}", "w") as f:
        f.write("data")
    return "ok"
`, models.KindShellInvocation)
	if !rules.PredHasWriteCall(tool, pf) {
		t.Error("expected HasWriteCall true")
	}
}

func TestPred_HasWriteCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
def read(name: str) -> str:
    """Read."""
    with open(f"/tmp/{name}", "r") as f:
        return f.read()
`, models.KindShellInvocation)
	if rules.PredHasWriteCall(tool, pf) {
		t.Error("expected HasWriteCall false")
	}
}

// ─── has_dynamic_url_call ─────────────────────────────────────────────────────

func TestPred_HasDynamicURLCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def fetch(url: str) -> dict:
    """Fetch."""
    return requests.get(url).json()
`, models.KindClaudeSDKTool)
	if !rules.PredHasDynamicURLCall(tool, pf) {
		t.Error("expected HasDynamicURLCall true")
	}
}

func TestPred_HasDynamicURLCall_False(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def fetch() -> dict:
    """Fetch."""
    return requests.get("https://api.example.com/data").json()
`, models.KindClaudeSDKTool)
	if rules.PredHasDynamicURLCall(tool, pf) {
		t.Error("expected HasDynamicURLCall false")
	}
}

// ─── call_without_kwarg ───────────────────────────────────────────────────────

func TestPred_CallWithoutKwarg_True(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id).json()
`, models.KindClaudeSDKTool)
	expr := rules.CallWithoutKwargExpr{
		Callees: []string{"requests.get", "requests.post"},
		Missing: "timeout",
	}
	if !rules.PredCallWithoutKwarg(expr, tool, pf) {
		t.Error("expected CallWithoutKwarg true")
	}
}

func TestPred_CallWithoutKwarg_False(t *testing.T) {
	tool, pf := parsePy(t, `
import requests
def get_invoice(id: str) -> dict:
    """Fetch invoice."""
    return requests.get("https://api.example.com/" + id, timeout=10).json()
`, models.KindClaudeSDKTool)
	expr := rules.CallWithoutKwargExpr{
		Callees: []string{"requests.get"},
		Missing: "timeout",
	}
	if rules.PredCallWithoutKwarg(expr, tool, pf) {
		t.Error("expected CallWithoutKwarg false when timeout present")
	}
}

// ─── call_with_kwarg_value ────────────────────────────────────────────────────

func TestPred_CallWithKwargValue_True(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(name: str) -> str:
    """Run."""
    subprocess.run(f"cmd {name}", shell=True)
    return "done"
`, models.KindShellInvocation)
	expr := rules.CallWithKwargValueExpr{
		CalleePrefix: "subprocess.",
		Kwarg:        "shell",
		Value:        "True",
	}
	if !rules.PredCallWithKwargValue(expr, tool, pf) {
		t.Error("expected CallWithKwargValue true for shell=True")
	}
}

func TestPred_CallWithKwargValue_False(t *testing.T) {
	tool, pf := parsePy(t, `
import subprocess
def run(name: str) -> str:
    """Run."""
    subprocess.run(["cmd", name])
    return "done"
`, models.KindShellInvocation)
	expr := rules.CallWithKwargValueExpr{
		CalleePrefix: "subprocess.",
		Kwarg:        "shell",
		Value:        "True",
	}
	if rules.PredCallWithKwargValue(expr, tool, pf) {
		t.Error("expected CallWithKwargValue false for list-form call")
	}
}

// ─── tool decorator predicates ────────────────────────────────────────────────

func TestPredToolDecoratorKwargValue(t *testing.T) {
	tool := models.ToolDef{Config: map[string]string{"strict_mode": "False"}}
	if !rules.PredToolDecoratorKwargValue(rules.ToolDecoratorKwargValueExpr{Kwarg: "strict_mode", Value: "False"}, tool) {
		t.Error("expected match")
	}
	if rules.PredToolDecoratorKwargValue(rules.ToolDecoratorKwargValueExpr{Kwarg: "strict_mode", Value: "True"}, tool) {
		t.Error("expected no match (value mismatch)")
	}
	if rules.PredToolDecoratorKwargValue(rules.ToolDecoratorKwargValueExpr{Kwarg: "other", Value: "False"}, tool) {
		t.Error("expected no match (kwarg absent)")
	}
}

func TestPredToolDecoratorKwargPresent(t *testing.T) {
	tool := models.ToolDef{Config: map[string]string{"strict_mode": "False"}}
	if !rules.PredToolDecoratorKwargPresent([]string{"strict_mode"}, tool) {
		t.Error("expected present")
	}
	if rules.PredToolDecoratorKwargPresent([]string{"failure_error_function"}, tool) {
		t.Error("expected not present")
	}
}

// ─── agent predicates ─────────────────────────────────────────────────────────

func TestPredAgentClass(t *testing.T) {
	a := models.AgentDef{Class: "Agent"}
	if !rules.PredAgentClass([]string{"Agent"}, a) {
		t.Error("expected match")
	}
	if rules.PredAgentClass([]string{"SandboxAgent"}, a) {
		t.Error("expected no match")
	}
}

func TestPredAgentKwargPresent(t *testing.T) {
	a := models.AgentDef{Kwargs: &models.KwargTree{
		Children: map[string]*models.KwargTree{
			"model": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"gpt-4"`}},
			"model_settings": {Children: map[string]*models.KwargTree{
				"tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"required"`}},
			}},
		},
	}}
	if !rules.PredAgentKwargPresent([]string{"model"}, a) {
		t.Error("expected model present")
	}
	if !rules.PredAgentKwargPresent([]string{"model_settings.tool_choice"}, a) {
		t.Error("expected dotted match")
	}
	if rules.PredAgentKwargPresent([]string{"nope"}, a) {
		t.Error("expected not present")
	}
}

func TestPredAgentKwargMissing(t *testing.T) {
	a := models.AgentDef{Kwargs: &models.KwargTree{
		Children: map[string]*models.KwargTree{
			"model": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"gpt-4"`}},
		},
	}}
	if !rules.PredAgentKwargMissing([]string{"input_guardrails"}, a) {
		t.Error("expected input_guardrails missing")
	}
	if rules.PredAgentKwargMissing([]string{"model"}, a) {
		t.Error("expected model NOT missing")
	}
}

func TestPredAgentKwargListEmpty(t *testing.T) {
	a := models.AgentDef{Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}}}
	if !rules.PredAgentKwargListEmpty([]string{"input_guardrails"}, a) {
		t.Error("expected list empty when kwarg absent")
	}
	a = models.AgentDef{Kwargs: &models.KwargTree{
		Children: map[string]*models.KwargTree{
			"input_guardrails": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
				{Kind: models.ExprNameRef, Text: "g"},
			}}},
		},
	}}
	if rules.PredAgentKwargListEmpty([]string{"input_guardrails"}, a) {
		t.Error("expected list NOT empty")
	}
}

func TestPredAgentKwargValue_Dotted(t *testing.T) {
	a := models.AgentDef{Kwargs: &models.KwargTree{
		Children: map[string]*models.KwargTree{
			"model_settings": {Children: map[string]*models.KwargTree{
				"tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"required"`}},
			}},
			"reset_tool_choice": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
		},
	}}
	if !rules.PredAgentKwargValue(rules.AgentKwargValueExpr{Kwarg: "model_settings.tool_choice", Value: "required"}, a) {
		t.Error("expected dotted match (after stripping quotes)")
	}
	if !rules.PredAgentKwargValue(rules.AgentKwargValueExpr{Kwarg: "reset_tool_choice", Value: "False"}, a) {
		t.Error("expected bool literal match")
	}
}

func TestPredAgentUsesToolKind(t *testing.T) {
	shellTool := &models.ToolDef{Kind: models.KindShellInvocation, Name: "run"}
	a := models.AgentDef{ToolRefs: []models.ToolRef{{Name: "run", Resolved: shellTool}}}
	inv := models.RepoInventory{}
	if !rules.PredAgentUsesToolKind([]string{"shell_invocation"}, a, inv) {
		t.Error("expected match against shell_invocation tool ref")
	}
	if rules.PredAgentUsesToolKind([]string{"mcp_tool"}, a, inv) {
		t.Error("expected no match")
	}
}

func TestPredAgentHandoffToClass(t *testing.T) {
	sub := &models.AgentDef{Class: "Agent"}
	a := models.AgentDef{HandoffRefs: []models.AgentRef{{Resolved: sub}}}
	if !rules.PredAgentHandoffToClass([]string{"Agent"}, a) {
		t.Error("expected match")
	}
	if rules.PredAgentHandoffToClass([]string{"SandboxAgent"}, a) {
		t.Error("expected no match")
	}
}

// ─── repo predicates ──────────────────────────────────────────────────────────

func TestPredRepoHasSDKDep(t *testing.T) {
	p := models.RepoProfile{SDKDeps: []models.SDKDep{{Name: "openai-agents"}}}
	if !rules.PredRepoHasSDKDep([]string{"openai-agents"}, p) {
		t.Error("expected match")
	}
	if rules.PredRepoHasSDKDep([]string{"langgraph"}, p) {
		t.Error("expected no match")
	}
}

func TestPredRepoHasSDKInCode(t *testing.T) {
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}}
	if !rules.PredRepoHasSDKInCode([]string{"openai_agents"}, inv) {
		t.Error("expected match")
	}
	if rules.PredRepoHasSDKInCode([]string{"claude_agent_sdk"}, inv) {
		t.Error("expected no match")
	}
}

func TestPredRepoHasAgentClass(t *testing.T) {
	inv := models.RepoInventory{Agents: []models.AgentDef{{Class: "Agent"}}}
	if !rules.PredRepoHasAgentClass([]string{"Agent"}, inv) {
		t.Error("expected match")
	}
	if rules.PredRepoHasAgentClass([]string{"SandboxAgent"}, inv) {
		t.Error("expected no match")
	}
}

func TestPredRepoHasNoAgentClass(t *testing.T) {
	inv := models.RepoInventory{Agents: []models.AgentDef{{Class: "Agent"}}}
	if rules.PredRepoHasNoAgentClass([]string{"Agent"}, inv) {
		t.Error("expected false when Agent exists")
	}
	if !rules.PredRepoHasNoAgentClass([]string{"SandboxAgent"}, inv) {
		t.Error("expected true when SandboxAgent absent")
	}
}

func TestPredRepoComponentPresent(t *testing.T) {
	p := models.RepoProfile{Manifest: models.ScanManifest{
		Components: []models.AgentComponent{{Kind: models.ComponentMCPConfig, Path: "mcp.json"}},
	}}
	if !rules.PredRepoComponentPresent([]string{"mcp_config"}, p) {
		t.Error("expected match")
	}
	if rules.PredRepoComponentPresent([]string{"hook_script"}, p) {
		t.Error("expected no match")
	}
}

func TestPredRepoUsesDefaultTracing(t *testing.T) {
	inv := models.RepoInventory{UsesDefaultTracing: true}
	if !rules.PredRepoUsesDefaultTracing(true, inv) {
		t.Error("expected default tracing = true")
	}
	inv.UsesDefaultTracing = false
	if rules.PredRepoUsesDefaultTracing(true, inv) {
		t.Error("expected default tracing = false after custom processor")
	}
}

func TestPredAgentUsesHostedToolClass(t *testing.T) {
	bashRef := models.HostedToolRef{
		Class:    "BashTool",
		Resolved: &models.HostedToolDef{Class: "BashTool"},
	}
	webRef := models.HostedToolRef{
		Class:    "WebSearchTool",
		Resolved: &models.HostedToolDef{Class: "WebSearchTool"},
	}
	cases := []struct {
		name    string
		agent   models.AgentDef
		classes []string
		want    bool
	}{
		{
			name:    "matches single class",
			agent:   models.AgentDef{HostedToolRefs: []models.HostedToolRef{bashRef}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "matches one of many",
			agent:   models.AgentDef{HostedToolRefs: []models.HostedToolRef{webRef, bashRef}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "no match",
			agent:   models.AgentDef{HostedToolRefs: []models.HostedToolRef{webRef}},
			classes: []string{"BashTool"},
			want:    false,
		},
		{
			name:    "unresolved ref still matches by class name",
			agent:   models.AgentDef{HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "no refs",
			agent:   models.AgentDef{},
			classes: []string{"BashTool"},
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rules.PredAgentUsesHostedToolClass(c.classes, c.agent)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ─── agent_is_subagent_of_any ─────────────────────────────────────────────────

func TestPredAgentIsSubagentOfAny(t *testing.T) {
	childResolved := &models.AgentDef{Name: "child", FilePath: "main.py"}
	parent := models.AgentDef{
		Name:     "parent",
		FilePath: "main.py",
		Class:    "LlmAgent",
		SDK:      models.SDKGoogleADK,
		HandoffRefs: []models.AgentRef{
			{Name: "child", Resolved: childResolved},
		},
	}
	selfParent := models.AgentDef{
		Name:     "selfparent",
		FilePath: "main.py",
		Class:    "LlmAgent",
		SDK:      models.SDKGoogleADK,
		HandoffRefs: []models.AgentRef{
			{Name: "selfparent", Resolved: &models.AgentDef{Name: "selfparent", FilePath: "main.py"}},
		},
	}

	cases := []struct {
		name  string
		agent models.AgentDef
		inv   models.RepoInventory
		want  bool
	}{
		{
			name:  "child appears in parent's HandoffRefs",
			agent: models.AgentDef{Name: "child", FilePath: "main.py"},
			inv:   models.RepoInventory{Agents: []models.AgentDef{parent}},
			want:  true,
		},
		{
			name:  "unrelated agent is not anyone's subagent",
			agent: models.AgentDef{Name: "unrelated", FilePath: "main.py"},
			inv:   models.RepoInventory{Agents: []models.AgentDef{parent}},
			want:  false,
		},
		{
			name:  "self-handoff edge case — still true",
			agent: models.AgentDef{Name: "selfparent", FilePath: "main.py"},
			inv:   models.RepoInventory{Agents: []models.AgentDef{selfParent}},
			want:  true,
		},
		{
			name:  "empty inventory",
			agent: models.AgentDef{Name: "child", FilePath: "main.py"},
			inv:   models.RepoInventory{},
			want:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rules.PredAgentIsSubagentOfAny(c.agent, c.inv)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
