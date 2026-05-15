package rules_test

import (
	"testing"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/analysis/astutil"
	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/rules"
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

// ─── call_uses_param ──────────────────────────────────────────────────────────

func TestPred_CallUsesParam_True(t *testing.T) {
	tool, pf := parsePy(t, `
def read_file(file_path: str) -> str:
    """Read a file."""
    with open(file_path, "r") as f:
        return f.read()
`, models.KindClaudeSDKTool)
	expr := rules.CallUsesParamExpr{
		Callees:        []string{"open", "Path"},
		CalleePrefixes: []string{"os.", "shutil."},
	}
	if !rules.PredCallUsesParam(expr, tool, pf) {
		t.Error("expected CallUsesParam true")
	}
}

func TestPred_CallUsesParam_False_NoPathishParam(t *testing.T) {
	tool, pf := parsePy(t, `
def get_editor(editor_id: str) -> dict:
    """Get editor."""
    return {"id": editor_id}
`, models.KindClaudeSDKTool)
	expr := rules.CallUsesParamExpr{
		Callees: []string{"open"},
	}
	if rules.PredCallUsesParam(expr, tool, pf) {
		t.Error("expected CallUsesParam false: 'editor_id' is not path-like")
	}
}
