package rules_test

import (
	"context"
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
		Name:     name,
		Kind:     kind,
		Language: models.LanguagePython,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     astutil.NodeLine(fn),
			EndLine:  astutil.NodeEndLine(fn),
		},
		Description:    doc,
		HasTypedParams: astutil.FunctionHasTypedParams(fn, b),
		ParamNames:     filtered,
		Facts:          map[string]string{},
	}
	return tool, pf
}

// parseTSTool parses a TypeScript snippet and runs the TS tool discovery
// matching `kind`, returning the first discovered tool plus its ParsedFile.
// Mirrors parsePy for the TS body/fact predicates and the TS rule harness.
func parseTSTool(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse TS: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "src/a.ts", Tree: tree, Source: []byte(src)}
	var tools []models.ToolDef
	switch kind {
	case models.KindClaudeSDKTool:
		tools = analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, func(string) {})
	case models.KindOpenAITool:
		tools = analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, func(string) {})
	case models.KindADKFunctionTool:
		tools = analysis.DiscoverTSADKTools([]analysis.ParsedFile{pf}, func(string) {})
	case models.KindMCPTool:
		tools = analysis.DiscoverTSMCPProper([]analysis.ParsedFile{pf}, func(string) {})
	case models.KindLangChainTool:
		tools = analysis.DiscoverTSLangChainTools([]analysis.ParsedFile{pf}, func(string) {})
	case models.KindVercelAITool:
		tools = analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, func(string) {})
	default:
		t.Fatalf("parseTSTool: unsupported kind %q", kind)
	}
	if len(tools) == 0 {
		t.Fatal("parseTSTool: no tool discovered (check the import gate in the snippet)")
	}
	return tools[0], pf
}

// parseGoTool parses a Go snippet and returns the first discovered Go MCP tool,
// mirroring parseTSTool for the language:go rule cases.
func parseGoTool(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	tree, err := astutil.NewGoParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse Go: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "main.go", Tree: tree, Source: []byte(src)}
	var tools []models.ToolDef
	switch kind {
	case models.KindMCPTool:
		tools = analysis.DiscoverGoMCPTools([]analysis.ParsedFile{pf}, func(string) {})
	default:
		t.Fatalf("parseGoTool: unsupported kind %q", kind)
	}
	if len(tools) == 0 {
		t.Fatal("parseGoTool: no tool discovered (check the import gate in the snippet)")
	}
	return tools[0], pf
}

// parseCSharpTool parses a C# snippet and returns the first discovered C# MCP
// tool, mirroring parseGoTool for the language:csharp rule cases.
func parseCSharpTool(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	tree, err := astutil.NewCSharpParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse C#: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "Tools.cs", Tree: tree, Source: []byte(src)}
	var tools []models.ToolDef
	switch kind {
	case models.KindMCPTool:
		tools = analysis.DiscoverCSharpMCPTools([]analysis.ParsedFile{pf}, func(string) {})
	default:
		t.Fatalf("parseCSharpTool: unsupported kind %q", kind)
	}
	if len(tools) == 0 {
		t.Fatal("parseCSharpTool: no tool discovered (check the using gate in the snippet)")
	}
	return tools[0], pf
}

// parsePHPTool parses a PHP snippet and returns the first discovered PHP MCP
// tool, mirroring parseCSharpTool for the language:php rule cases.
func parsePHPTool(t *testing.T, src string, kind models.ToolKind) (models.ToolDef, analysis.ParsedFile) {
	t.Helper()
	tree, err := astutil.NewPHPParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse PHP: %v", err)
	}
	pf := analysis.ParsedFile{RelPath: "Tools.php", Tree: tree, Source: []byte(src)}
	var tools []models.ToolDef
	switch kind {
	case models.KindMCPTool:
		tools = analysis.DiscoverPHPMCPTools([]analysis.ParsedFile{pf}, func(string) {})
	default:
		t.Fatalf("parsePHPTool: unsupported kind %q", kind)
	}
	if len(tools) == 0 {
		t.Fatal("parsePHPTool: no tool discovered (check the use gate in the snippet)")
	}
	return tools[0], pf
}

// parseTSAgentInline parses a TS snippet and returns the first discovered
// Claude agent. For use in package-level agent-rule case tables (no *testing.T
// available), so it panics rather than failing a test.
func parseTSAgentInline(src string) models.AgentDef {
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		panic("parseTSAgentInline parse: " + err.Error())
	}
	pf := analysis.ParsedFile{RelPath: "src/a.ts", Tree: tree, Source: []byte(src)}
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, func(string) {})
	if len(agents) == 0 {
		panic("parseTSAgentInline: no agent discovered")
	}
	return agents[0]
}

// parseTSOpenAIAgentInline parses a TS snippet and returns the first discovered
// OpenAI Agents SDK agent. HostedToolRefs are pre-resolved during discovery, so
// no ResolveEdges pass is needed for hosted-tool-class rules. Panics (no
// *testing.T available in package-level case tables).
func parseTSOpenAIAgentInline(src string) models.AgentDef {
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		panic("parseTSOpenAIAgentInline parse: " + err.Error())
	}
	pf := analysis.ParsedFile{RelPath: "src/a.ts", Tree: tree, Source: []byte(src)}
	agents := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, func(string) {})
	if len(agents) == 0 {
		panic("parseTSOpenAIAgentInline: no agent discovered")
	}
	return agents[0]
}

// parseTSADKAgentInline parses a TS snippet and returns the first discovered
// Google ADK agent. Panics (no *testing.T available in package-level case
// tables).
func parseTSADKAgentInline(src string) models.AgentDef {
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		panic("parseTSADKAgentInline parse: " + err.Error())
	}
	pf := analysis.ParsedFile{RelPath: "src/a.ts", Tree: tree, Source: []byte(src)}
	agents := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, func(string) {})
	if len(agents) == 0 {
		panic("parseTSADKAgentInline: no agent discovered")
	}
	return agents[0]
}

// parseTSVercelAgentInline parses a TS snippet and returns the first discovered
// Vercel AI SDK agent. HostedToolRefs carry their canonical Class at discovery
// (agent_uses_hosted_tool_class matches on Class, not Resolved), so no
// ResolveEdges pass is needed for the hosted-tool-class rules. Panics (no
// *testing.T available in package-level case tables).
func parseTSVercelAgentInline(src string) models.AgentDef {
	tree, err := astutil.NewTSParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		panic("parseTSVercelAgentInline parse: " + err.Error())
	}
	pf := analysis.ParsedFile{RelPath: "src/a.ts", Tree: tree, Source: []byte(src)}
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, func(string) {})
	if len(agents) == 0 {
		panic("parseTSVercelAgentInline: no agent discovered")
	}
	return agents[0]
}

func TestParseTSTool_Smoke(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("fetcher", "fetches", { url: z.string() }, async ({ url }) => {
  const r = await fetch(url);
  return { content: [] };
});
`
	tool, pf := parseTSTool(t, src, models.KindClaudeSDKTool)
	if tool.Language != models.LanguageTypeScript {
		t.Errorf("Language = %q, want typescript", tool.Language)
	}
	if tool.EndLine < tool.Line {
		t.Errorf("EndLine %d < Line %d (span needed by has_body_text fallback)", tool.EndLine, tool.Line)
	}
	if pf.Tree == nil {
		t.Error("nil tree")
	}
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

func TestPred_HasBodyText_TSFallback_Hit(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("runner", "runs", { cmd: z.string() }, async ({ cmd }) => {
  const { execSync } = require("child_process");
  execSync(cmd);
  return { content: [] };
});
`
	tool, pf := parseTSTool(t, src, models.KindClaudeSDKTool)
	if !rules.PredHasBodyText([]string{"execSync"}, tool, pf) {
		t.Error("expected has_body_text to hit execSync in TS tool span")
	}
}

func TestPred_HasBodyText_TSFallback_Miss(t *testing.T) {
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("greet", "greets", { name: z.string() }, async ({ name }) => {
  return { content: [{ type: "text", text: "hi " + name }] };
});
`
	tool, pf := parseTSTool(t, src, models.KindClaudeSDKTool)
	if rules.PredHasBodyText([]string{"execSync"}, tool, pf) {
		t.Error("expected has_body_text miss on a tool with no execSync")
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

func TestPred_HasShellCall_OsSpawn(t *testing.T) {
	// os.spawn* spawns a process just like subprocess/os.system; the predicate
	// must catch the whole os.spawn family, not only system/popen.
	tool, pf := parsePy(t, `
import os
def run(cmd: str) -> str:
    """Run."""
    os.spawnl(os.P_WAIT, "/bin/ls", "ls")
    return "done"
`, models.KindShellInvocation)
	if !rules.PredHasShellCall(tool, pf) {
		t.Error("expected HasShellCall true for os.spawnl")
	}
}

func TestPred_HasCodeExecCall_True(t *testing.T) {
	for _, src := range []string{
		"\ndef t(x: str):\n    \"\"\"d.\"\"\"\n    return eval(x)\n",
		"\ndef t(x: str):\n    \"\"\"d.\"\"\"\n    exec(x)\n",
		"\ndef t(x: str):\n    \"\"\"d.\"\"\"\n    return compile(x, \"<s>\", \"exec\")\n",
	} {
		tool, pf := parsePy(t, src, models.KindOpenAITool)
		if !rules.PredHasCodeExecCall(tool, pf) {
			t.Errorf("expected HasCodeExecCall true for: %q", src)
		}
	}
}

func TestPred_HasCodeExecCall_ReCompileIsSafe(t *testing.T) {
	// re.compile is a safe stdlib call. The structured predicate matches the
	// bare builtin `compile`, not the `re.compile` attribute call — this is the
	// false positive that substring matching on "compile(" cannot avoid.
	tool, pf := parsePy(t, `
import re
def t(pattern: str):
    """d."""
    return re.compile(pattern)
`, models.KindOpenAITool)
	if rules.PredHasCodeExecCall(tool, pf) {
		t.Error("expected HasCodeExecCall false for re.compile")
	}
}

func TestPred_HasCodeExecCall_None(t *testing.T) {
	tool, pf := parsePy(t, `
def t(x: str) -> str:
    """d."""
    return x.upper()
`, models.KindOpenAITool)
	if rules.PredHasCodeExecCall(tool, pf) {
		t.Error("expected HasCodeExecCall false")
	}
}

func TestEvaluateTool_DispatchesHasCodeExecCall(t *testing.T) {
	// Guards that EvaluateTool actually wires the predicate — a field present in
	// the scope maps but not dispatched would be a silent no-op.
	tool, pf := parsePy(t, `
def t(x: str):
    """d."""
    return eval(x)
`, models.KindOpenAITool)
	tru := true
	expr := rules.MatchExpr{HasCodeExecCall: &tru}
	if !expr.EvaluateTool(tool, pf) {
		t.Error("EvaluateTool should dispatch has_code_exec_call and match eval()")
	}
}

func TestPred_HasPrintCall_True(t *testing.T) {
	tool, pf := parsePy(t, `
def t(x: str):
    """d."""
    print("debug", x)
    return x
`, models.KindOpenAITool)
	if !rules.PredHasPrintCall(tool, pf) {
		t.Error("expected HasPrintCall true for print()")
	}
}

func TestPred_HasPrintCall_PprintIsSafe(t *testing.T) {
	// pprint() contains the substring "print(" but is a distinct callee. The
	// structured predicate matches the bare `print` builtin, not any callee
	// whose text happens to contain "print(" — the false positive that
	// substring matching cannot avoid.
	tool, pf := parsePy(t, `
from pprint import pprint
def t(x: dict):
    """d."""
    pprint(x)
    return x
`, models.KindOpenAITool)
	if rules.PredHasPrintCall(tool, pf) {
		t.Error("expected HasPrintCall false for pprint()")
	}
}

func TestPred_HasPrintCall_None(t *testing.T) {
	tool, pf := parsePy(t, `
def t(x: str) -> str:
    """d."""
    return x.upper()
`, models.KindOpenAITool)
	if rules.PredHasPrintCall(tool, pf) {
		t.Error("expected HasPrintCall false")
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

func TestPred_HasDynamicURLCall_TS(t *testing.T) {
	hit := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", { host: z.string() }, async ({ host }) => {
  await fetch(` + "`https://${host}/x`" + `);
  return { content: [] };
});
`
	tool, pf := parseTSTool(t, hit, models.KindClaudeSDKTool)
	if !rules.PredHasDynamicURLCall(tool, pf) {
		t.Error("expected dynamic-url predicate to fire on TS interpolated fetch")
	}

	miss := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const t = tool("f", "f", {}, async () => {
  await fetch("https://example.com");
  return { content: [] };
});
`
	tool2, pf2 := parseTSTool(t, miss, models.KindClaudeSDKTool)
	if rules.PredHasDynamicURLCall(tool2, pf2) {
		t.Error("expected dynamic-url predicate silent on TS literal fetch")
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
	a := models.AgentDef{Class: "Agent", Language: models.LanguagePython}
	if !rules.PredAgentClass([]string{"Agent"}, a) {
		t.Error("expected match")
	}
	if rules.PredAgentClass([]string{"SandboxAgent"}, a) {
		t.Error("expected no match")
	}
}

func TestPredAgentKwargPresent(t *testing.T) {
	a := models.AgentDef{
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
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
	a := models.AgentDef{
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
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
	a := models.AgentDef{Language: models.LanguagePython, Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}}}
	if !rules.PredAgentKwargListEmpty([]string{"input_guardrails"}, a) {
		t.Error("expected list empty when kwarg absent")
	}
	a = models.AgentDef{
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
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
	a := models.AgentDef{
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
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
	a := models.AgentDef{Language: models.LanguagePython, ToolRefs: []models.ToolRef{{Name: "run", Resolved: shellTool}}}
	inv := models.RepoInventory{}
	if !rules.PredAgentUsesToolKind([]string{"shell_invocation"}, a, inv) {
		t.Error("expected match against shell_invocation tool ref")
	}
	if rules.PredAgentUsesToolKind([]string{"mcp_tool"}, a, inv) {
		t.Error("expected no match")
	}

	// E2: a hosted ShellTool (no ToolDef/Kind) maps to shell_invocation.
	hosted := models.AgentDef{Language: models.LanguagePython,
		HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}}}
	if !rules.PredAgentUsesToolKind([]string{"shell_invocation"}, hosted, inv) {
		t.Error("expected hosted ShellTool to match shell_invocation kind")
	}
	webOnly := models.AgentDef{Language: models.LanguagePython,
		HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}}}
	if rules.PredAgentUsesToolKind([]string{"shell_invocation"}, webOnly, inv) {
		t.Error("expected hosted WebSearchTool NOT to match shell_invocation")
	}

	// E3: a decorated tool (Kind openai_tool) that shells out carries a
	// structural shells_out fact; shell_invocation must match it even though
	// its Kind is not shell_invocation.
	shellDecorated := &models.ToolDef{Kind: models.KindOpenAITool,
		Facts: map[string]string{"shells_out": "true"}}
	aDec := models.AgentDef{Language: models.LanguagePython,
		ToolRefs: []models.ToolRef{{Resolved: shellDecorated}}}
	if !rules.PredAgentUsesToolKind([]string{"shell_invocation"}, aDec, inv) {
		t.Error("expected decorated tool with shells_out fact to match shell_invocation")
	}
	pureDecorated := &models.ToolDef{Kind: models.KindOpenAITool, Facts: map[string]string{}}
	aPure := models.AgentDef{Language: models.LanguagePython,
		ToolRefs: []models.ToolRef{{Resolved: pureDecorated}}}
	if rules.PredAgentUsesToolKind([]string{"shell_invocation"}, aPure, inv) {
		t.Error("expected non-shelling decorated tool NOT to match shell_invocation")
	}
}

// ─── repo predicates ──────────────────────────────────────────────────────────

func TestPredRepoHasSDKInCode(t *testing.T) {
	inv := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}}
	if !rules.PredRepoHasSDKInCode([]string{"openai_agents"}, inv) {
		t.Error("expected match")
	}
	if rules.PredRepoHasSDKInCode([]string{"claude_agent_sdk"}, inv) {
		t.Error("expected no match")
	}
}

func TestPredRepoHasSDKInCode_OpenshellRoutesToShellInvocations(t *testing.T) {
	// "openshell" must NOT match via SDKsDetected even if a (broken) caller
	// were to put it there — it is only true when HasShellInvocations is true.
	noShell := models.RepoInventory{SDKsDetected: []models.SDK{models.SDKOpenAIAgents}}
	if rules.PredRepoHasSDKInCode([]string{"openshell"}, noShell) {
		t.Error("openshell must not match when HasShellInvocations=false")
	}
	withShell := models.RepoInventory{HasShellInvocations: true}
	if !rules.PredRepoHasSDKInCode([]string{"openshell"}, withShell) {
		t.Error("openshell must match when HasShellInvocations=true, even with empty SDKsDetected")
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
			agent:   models.AgentDef{Language: models.LanguagePython, HostedToolRefs: []models.HostedToolRef{bashRef}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "matches one of many",
			agent:   models.AgentDef{Language: models.LanguagePython, HostedToolRefs: []models.HostedToolRef{webRef, bashRef}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "no match",
			agent:   models.AgentDef{Language: models.LanguagePython, HostedToolRefs: []models.HostedToolRef{webRef}},
			classes: []string{"BashTool"},
			want:    false,
		},
		{
			name:    "unresolved ref still matches by class name",
			agent:   models.AgentDef{Language: models.LanguagePython, HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}}},
			classes: []string{"BashTool"},
			want:    true,
		},
		{
			name:    "no refs",
			agent:   models.AgentDef{Language: models.LanguagePython},
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

// hostedRefWithKwargs builds a resolved HostedToolRef of class with one kwarg.
func hostedRefWithKwargs(class string, kwargs map[string]*models.KwargTree) models.HostedToolRef {
	return models.HostedToolRef{
		Class:    class,
		Resolved: &models.HostedToolDef{Class: class, Kwargs: &models.KwargTree{Children: kwargs}},
	}
}

func TestPredAgentHostedToolKwargPresent(t *testing.T) {
	withPolicy := models.AgentDef{HostedToolRefs: []models.HostedToolRef{
		hostedRefWithKwargs("BashTool", map[string]*models.KwargTree{
			"policy": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "p"}},
		}),
	}}
	noPolicy := models.AgentDef{HostedToolRefs: []models.HostedToolRef{
		hostedRefWithKwargs("BashTool", map[string]*models.KwargTree{}),
	}}
	unresolved := models.AgentDef{HostedToolRefs: []models.HostedToolRef{{Class: "BashTool"}}}

	expr := rules.HostedToolKwargExpr{Class: "BashTool", Kwarg: "policy"}
	if !rules.PredAgentHostedToolKwargPresent(expr, withPolicy) {
		t.Error("expected present=true when BashTool has policy")
	}
	if rules.PredAgentHostedToolKwargPresent(expr, noPolicy) {
		t.Error("expected present=false when BashTool has no policy")
	}
	if rules.PredAgentHostedToolKwargPresent(expr, unresolved) {
		t.Error("expected present=false when ref unresolved (kwargs unavailable)")
	}
}

func TestPredAgentHostedToolKwargValue(t *testing.T) {
	approved := models.AgentDef{HostedToolRefs: []models.HostedToolRef{
		hostedRefWithKwargs("ShellTool", map[string]*models.KwargTree{
			"needs_approval": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
		}),
	}}
	unapproved := models.AgentDef{HostedToolRefs: []models.HostedToolRef{
		hostedRefWithKwargs("ShellTool", map[string]*models.KwargTree{
			"needs_approval": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
		}),
	}}
	expr := rules.HostedToolKwargValueExpr{Class: "ShellTool", Kwarg: "needs_approval", Value: "True"}
	if !rules.PredAgentHostedToolKwargValue(expr, approved) {
		t.Error("expected value match when needs_approval=True")
	}
	if rules.PredAgentHostedToolKwargValue(expr, unapproved) {
		t.Error("expected no match when needs_approval=False")
	}
}

// ─── agent_is_subagent_of_any ─────────────────────────────────────────────────

// ─── call_without_kwarg: alias-aware + None-aware ────────────────────────────

func TestPredCallWithoutKwarg_AliasAndNone(t *testing.T) {
	expr := rules.CallWithoutKwargExpr{
		Callees: []string{"requests.get", "requests.post", "httpx.post"},
		Missing: "timeout",
	}
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"session alias fires", `
def tool(url: str) -> str:
    """t."""
    s = requests.Session()
    return s.get(url).text
`, true},
		{"session alias with timeout silent", `
def tool(url: str) -> str:
    """t."""
    s = requests.Session()
    return s.get(url, timeout=10).text
`, false},
		{"with-binding fires", `
def tool(url: str) -> str:
    """t."""
    with httpx.Client() as c:
        return c.post(url).text
`, true},
		{"timeout=None fires", `
def tool(url: str) -> str:
    """t."""
    return requests.get(url, timeout=None).text
`, true},
		{"direct with timeout still silent", `
def tool(url: str) -> str:
    """t."""
    return requests.get(url, timeout=10).text
`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, pf := parsePy(t, c.src, models.KindOpenAITool)
			got := rules.PredCallWithoutKwarg(expr, tool, pf)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestPredHasDynamicURLCall_Alias(t *testing.T) {
	src := `
def tool(host: str) -> str:
    """t."""
    s = requests.Session()
    return s.get(f"https://{host}/x").text
`
	tool, pf := parsePy(t, src, models.KindOpenAITool)
	if !rules.PredHasDynamicURLCall(tool, pf) {
		t.Errorf("expected dynamic-URL call through alias to be detected")
	}
}

func TestPredAgentIsSubagentOfAny(t *testing.T) {
	childResolved := &models.AgentDef{Name: "child", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython}
	parent := models.AgentDef{
		SDK:      models.SDKGoogleADK,
		Class:    "LlmAgent",
		Language: models.LanguagePython,
		Location: models.Location{FilePath: "main.py"},
		Name:     "parent",
		HandoffRefs: []models.AgentRef{
			{Name: "child", Resolved: childResolved},
		},
	}
	selfParent := models.AgentDef{
		SDK:      models.SDKGoogleADK,
		Class:    "LlmAgent",
		Language: models.LanguagePython,
		Location: models.Location{FilePath: "main.py"},
		Name:     "selfparent",
		HandoffRefs: []models.AgentRef{
			{Name: "selfparent", Resolved: &models.AgentDef{Name: "selfparent", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython}},
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
			agent: models.AgentDef{Name: "child", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython},
			inv:   models.RepoInventory{Agents: []models.AgentDef{parent}},
			want:  true,
		},
		{
			name:  "unrelated agent is not anyone's subagent",
			agent: models.AgentDef{Name: "unrelated", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython},
			inv:   models.RepoInventory{Agents: []models.AgentDef{parent}},
			want:  false,
		},
		{
			name:  "self-handoff edge case — still true",
			agent: models.AgentDef{Name: "selfparent", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython},
			inv:   models.RepoInventory{Agents: []models.AgentDef{selfParent}},
			want:  true,
		},
		{
			name:  "empty inventory",
			agent: models.AgentDef{Name: "child", Location: models.Location{FilePath: "main.py"}, Language: models.LanguagePython},
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

func TestPredAgentKwargMissing_None(t *testing.T) {
	mk := func(kind models.ExprKind, text string) models.AgentDef {
		return models.AgentDef{
			Language: models.LanguagePython,
			Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
				"before_tool_callback": {Value: &models.Expr{Kind: kind, Text: text}},
			}},
		}
	}
	// present with None -> counts as missing
	if !rules.PredAgentKwargMissing([]string{"before_tool_callback"}, mk(models.ExprLiteralNone, "None")) {
		t.Errorf("before_tool_callback=None should count as missing")
	}
	// present with a real value -> not missing
	if rules.PredAgentKwargMissing([]string{"before_tool_callback"}, mk(models.ExprNameRef, "my_fn")) {
		t.Errorf("before_tool_callback=my_fn should NOT count as missing")
	}
	// absent -> missing
	if !rules.PredAgentKwargMissing([]string{"before_tool_callback"}, models.AgentDef{Language: models.LanguagePython, Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}}}) {
		t.Errorf("absent before_tool_callback should count as missing")
	}
}

func TestPredSubagentGrantsTool(t *testing.T) {
	grants := models.SubagentDef{Name: "x", Tools: []string{"Read", "Bash", "Grep"}}
	noBash := models.SubagentDef{Name: "y", Tools: []string{"Read", "Grep"}}
	none := models.SubagentDef{Name: "z"}

	if !rules.PredSubagentGrantsTool(grants, []string{"Bash"}) {
		t.Errorf("expected true: Tools contains Bash")
	}
	if rules.PredSubagentGrantsTool(noBash, []string{"Bash"}) {
		t.Errorf("expected false: Tools does not contain Bash")
	}
	if rules.PredSubagentGrantsTool(none, []string{"Bash"}) {
		t.Errorf("expected false: no Tools")
	}
	if !rules.PredSubagentGrantsTool(noBash, []string{"Bash", "Grep"}) {
		t.Errorf("expected true: Tools contains Grep (one of the listed)")
	}

	// A parametered grant ("Bash(npm run *)") must match the name "Bash" via the
	// parsed ToolGrants, even though the raw Tools token is not exactly "Bash".
	parametered := models.SubagentDef{
		Name:       "p",
		Tools:      []string{"Bash(npm run *)"},
		ToolGrants: []models.ToolGrant{{Tool: "Bash", Pattern: "npm run *", Raw: "Bash(npm run *)"}},
	}
	if !rules.PredSubagentGrantsTool(parametered, []string{"Bash"}) {
		t.Errorf("expected true: Bash(npm run *) grants Bash via ToolGrants")
	}
}
