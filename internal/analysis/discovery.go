// Package analysis implements Tool Discovery + Detector Suite + Scoring
// (architecture §2).
package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// ParsedFile pairs a file's source bytes with its tree-sitter root.
// We hand this to detectors so they don't re-parse.
type ParsedFile struct {
	RelPath string
	Source  []byte
	Tree    *sitter.Tree
}

// DiscoverToolsFromParsed runs tool discovery on pre-parsed files. Used by
// tests and by ResolveEdges which already holds ParsedFile objects.
func DiscoverToolsFromParsed(parsed []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range parsed {
		out = append(out, toolsInFile(pf)...)
	}
	return out
}

// DiscoverTools walks the manifest's Python files, parses each, and extracts
// ToolDefs.
//
// Three recognition strategies, in priority order:
//
//  1. Claude Agent SDK `@tool` decorator (most reliable signal).
//  2. MCP server registrations: `server.tool("name", ...)` or
//     `@server.tool(...)`. (Spec: github.com/modelcontextprotocol/python-sdk.)
//  3. Shell-invocation hotspots: any function that calls subprocess.* or
//     os.system. These get KindShellInvocation and feed the OpenShell detectors.
//
// A single function can be classified as both a tool AND a shell-invocation —
// in that case it becomes KindClaudeSDKTool with a fact tagging the shell call.
// The returned skipped slice names the relative paths that could not be read
// or parsed. One bad file must not abort the scan, but the skip must not be
// invisible either: the caller folds these into Coverage so the report can name
// the files it did not analyze (a silent drop on a security tool is the "clean
// report that isn't" failure mode). The error return is reserved for a future
// fatal condition; it is always nil today.
func DiscoverTools(manifest models.ScanManifest, onFile func(path string)) ([]models.ToolDef, []ParsedFile, []string, error) {
	var tools []models.ToolDef
	var parsed []ParsedFile
	var skipped []string

	// Reuse one parser across every file instead of allocating a fresh C parser
	// per file (the prior astutil.Parse-per-file pattern). Parsers are reusable
	// and not shared across goroutines; this loop is single-threaded.
	parser := astutil.NewPyParser()

	for _, rel := range manifest.PythonFiles {
		if onFile != nil {
			onFile(rel)
		}
		abs := filepath.Join(manifest.RepoRoot, rel)
		src, err := os.ReadFile(abs)
		if err != nil {
			skipped = append(skipped, rel) // unreadable (perms, races) — not analyzed
			continue
		}
		tree, err := parser.ParseCtx(context.Background(), nil, src)
		if err != nil {
			skipped = append(skipped, rel) // unparseable — not analyzed
			continue
		}
		pf := ParsedFile{RelPath: rel, Source: src, Tree: tree}
		parsed = append(parsed, pf)
		tools = append(tools, toolsInFile(pf)...)
	}
	return tools, parsed, skipped, nil
}

func toolsInFile(pf ParsedFile) []models.ToolDef {
	root := pf.Tree.RootNode()
	var out []models.ToolDef

	// Pass 1: decorated functions.
	for _, dec := range astutil.FindAll(root, "decorated_definition") {
		fn := astutil.FunctionDef(dec)
		if fn == nil {
			continue
		}
		kind := kindFromDecorators(astutil.Decorators(dec), pf.Source)
		if kind == models.KindUnknown {
			continue
		}
		tool := buildTool(fn, pf, kind)
		tool.Config = extractDecoratorKwargs(dec, pf.Source)
		out = append(out, tool)
	}

	// Pass 2: bare function_definitions that call subprocess/os.system but
	// aren't already captured above. These are "shell invocation" surfaces;
	// the OpenShell detectors will inspect them.
	captured := map[int]bool{}
	for _, t := range out {
		captured[t.Line] = true
	}
	for _, fn := range astutil.FindAll(root, "function_definition") {
		line := astutil.NodeLine(fn)
		if captured[line] {
			continue
		}
		if !callsShell(fn, pf.Source) {
			continue
		}
		out = append(out, buildTool(fn, pf, models.KindShellInvocation))
	}

	return out
}

// kindFromDecorators inspects decorator nodes and decides whether this looks
// like a Claude Agent SDK tool, an OpenAI Agents SDK tool, an MCP tool, or
// neither. Conservative: when in doubt, return Unknown — a false negative is
// fixable by adding a recognizer; a false positive triggers detectors on user
// code that isn't even a tool.
//
// Order matters: @function_tool is OpenAI Agents SDK and is checked before
// the more permissive "@tool" Claude SDK match (which would otherwise swallow
// "@function_tool" via substring).
func kindFromDecorators(decs []*sitter.Node, src []byte) models.ToolKind {
	for _, d := range decs {
		// Match on the decorator's resolved callee path (e.g. "function_tool",
		// "agent.tool", "server.tool", "app.register_tool"), not a substring of
		// the raw decorator text. Substring matching mis-fired: "@tool" is a
		// prefix of "@tool_registry.register" and "@toolbar", so unrelated user
		// decorators were classified as Claude-SDK tools and triggered tool rules
		// on code that is not a tool at all.
		callee := decoratorCallee(d, src)
		if callee == "" {
			continue
		}
		last := callee
		if i := strings.LastIndex(callee, "."); i >= 0 {
			last = callee[i+1:]
		}
		switch {
		// OpenAI Agents SDK — `@function_tool` / `@function_tool(...)`, bare or
		// module-qualified (`agents.function_tool`).
		case callee == "function_tool" || last == "function_tool":
			return models.KindOpenAITool
		// Claude Agent SDK conventions. Real names are still in flux — CSDK is
		// pre-1.0. Expand this list as the SDK stabilizes.
		case callee == "tool" || callee == "claude_tool" || last == "claude_tool",
			callee == "agent.tool",
			strings.Contains(callee, "claude_agent_sdk"):
			return models.KindClaudeSDKTool
		// MCP registrations.
		case callee == "server.tool" || callee == "mcp.tool",
			last == "register_tool":
			return models.KindMCPTool
		}
	}
	return models.KindUnknown
}

// decoratorCallee returns the dotted callee path of a decorator, stripped of any
// call arguments: "@function_tool" → "function_tool", "@server.tool" →
// "server.tool", "@app.register_tool(x)" → "app.register_tool". Returns "" for a
// nil or non-decorator node.
func decoratorCallee(d *sitter.Node, src []byte) string {
	if d == nil || d.Type() != "decorator" {
		return ""
	}
	expr := d.NamedChild(0)
	if expr == nil {
		return ""
	}
	// `@deco(args)` — unwrap the call to its function (the callee identifier or
	// attribute), discarding the arguments.
	if expr.Type() == "call" {
		expr = expr.ChildByFieldName("function")
	}
	return astutil.NodeText(expr, src)
}

// callsShell returns true if any descendant of fn is a call to
// subprocess.{run,Popen,call,check_output,check_call} or os.system.
func callsShell(fn *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fnNode := n.ChildByFieldName("function")
		if fnNode == nil {
			return true
		}
		txt := astutil.NodeText(fnNode, src)
		if strings.HasPrefix(txt, "subprocess.") || txt == "os.system" || txt == "os.popen" {
			found = true
			return false
		}
		return true
	})
	return found
}

// extractDecoratorKwargs collects keyword arguments from the first decorator
// call in a decorated_definition (e.g. @function_tool(strict_mode=False)).
func extractDecoratorKwargs(dec *sitter.Node, src []byte) map[string]string {
	config := map[string]string{}
	for i := 0; i < int(dec.ChildCount()); i++ {
		decoratorNode := dec.Child(i)
		if decoratorNode.Type() != "decorator" {
			continue
		}
		body := decoratorNode.NamedChild(0)
		if body == nil || body.Type() != "call" {
			continue
		}
		args := body.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		for j := 0; j < int(args.ChildCount()); j++ {
			arg := args.Child(j)
			if arg.Type() != "keyword_argument" {
				continue
			}
			name := astutil.NodeText(arg.ChildByFieldName("name"), src)
			value := astutil.NodeText(arg.ChildByFieldName("value"), src)
			config[name] = value
		}
	}
	return config
}

// toolParamNames returns a function's parameter names with the implicit
// receiver (`self`/`cls`) dropped. A tool registered as a method still parses as
// a function_definition, so without this strip a phantom `self` param leaks into
// ParamNames and skews any param-counting tool rule (untyped-params, arity).
// Shared by every Python tool-discovery path so they agree on the param list.
func toolParamNames(fn *sitter.Node, src []byte) []string {
	params := astutil.FunctionParams(fn, src)
	filtered := params[:0]
	for _, p := range params {
		if p == "self" || p == "cls" {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func buildTool(fn *sitter.Node, pf ParsedFile, kind models.ToolKind) models.ToolDef {
	name := astutil.FunctionName(fn, pf.Source)
	filtered := toolParamNames(fn, pf.Source)

	facts := map[string]string{}
	// Stamp a structural shells_out fact so agent-scope rules can tell that a
	// decorated tool (Kind openai_tool / claude_sdk_tool / adk_function_tool)
	// shells out, without re-parsing. TS discovery already sets this via
	// tsHandlerFacts; this is the Python equivalent. Without it,
	// agent_uses_tool_kind:[shell_invocation] only catches BARE shell functions
	// (KindShellInvocation), missing the common @function_tool-wraps-subprocess
	// shape the agent shell-tool rules (OAI-101/104) promise to flag.
	if pythonBodyShellsOut(fn, pf.Source) {
		facts["shells_out"] = "true"
	}

	return models.ToolDef{
		Name:     name,
		Kind:     kind,
		Language: models.LanguagePython, // discovery is python-only today; widen when a TS parser lands
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     astutil.NodeLine(fn),
			EndLine:  astutil.NodeEndLine(fn),
		},
		Description:    astutil.FunctionDocstring(fn, pf.Source),
		HasTypedParams: astutil.FunctionHasTypedParams(fn, pf.Source),
		ParamNames:     filtered,
		Facts:          facts,
	}
}

// pythonBodyShellsOut reports whether the function body invokes an OS shell
// primitive (subprocess.*, os.system, os.popen, os.spawn*). Mirrors the callee
// set in rules.PredHasShellCall; kept here (not imported) because rules imports
// analysis, not the reverse.
func pythonBodyShellsOut(fn *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		callee := n.ChildByFieldName("function")
		if callee == nil {
			return true
		}
		c := astutil.NodeText(callee, src)
		if strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" ||
			strings.HasPrefix(c, "os.spawn") {
			found = true
			return false
		}
		return true
	})
	return found
}
