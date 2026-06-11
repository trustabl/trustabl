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
//  2. MCP server registrations via the DECORATOR form only:
//     `@server.tool(...)` / `@mcp.tool(...)` / `@*.register_tool(...)`. The
//     non-decorator call form (`server.tool("name", fn)`, `mcp.add_tool(fn)`)
//     is NOT recognized — only `decorated_definition` nodes are walked.
//     (Spec: github.com/modelcontextprotocol/python-sdk.)
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
func DiscoverTools(ctx context.Context, manifest models.ScanManifest, onFile func(path string)) ([]models.ToolDef, []ParsedFile, []string, error) {
	var tools []models.ToolDef
	var parsed []ParsedFile
	var skipped []string

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
		// A fresh parser per file: ParseCtxTimeout uses a cancelable context, and
		// go-tree-sitter's cancellation flag lives ON the parser. Reusing one
		// parser across files lets a parse's timeout goroutine set that flag after
		// the parse returns (a race with parseComplete), which then silently aborts
		// the NEXT file's parse — discovery would go empty. A per-file parser keeps
		// each parse's cancellation isolated; construction is cheap vs. parsing.
		parser := astutil.NewPyParser()
		tree, err := astutil.ParseCtxTimeout(ctx, parser, src)
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

	// `@tool` is exported by BOTH the Claude Agent SDK and LangChain. Resolve each
	// decorator by the import binding of the exact name it uses — `tool` from a
	// langchain module is a LangChain tool, `tool` from `claude_agent_sdk` is a
	// Claude tool — which is correct for any mix of the two SDKs and follows
	// Python's last-binding-wins shadowing. The file-level flags are only a
	// fallback for the unresolvable case (a star-import or a locally-defined
	// `tool`). Computed once per file.
	toolImports := collectToolImports(pf)
	lcImport := fileImportsLangChain(pf)
	claudeImport := fileImportsClaudeSDK(pf)
	crewaiImport := fileImportsCrewAI(pf)
	pydanticImport := fileImportsPydanticAI(pf)

	// Pass 1: decorated functions.
	for _, dec := range astutil.FindAll(root, "decorated_definition") {
		fn := astutil.FunctionDef(dec)
		if fn == nil {
			continue
		}
		kind := kindFromDecorators(astutil.Decorators(dec), pf.Source, toolImports, lcImport, claudeImport, crewaiImport, pydanticImport)
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
func kindFromDecorators(decs []*sitter.Node, src []byte, toolImports map[string]models.ToolKind, lcImport, claudeImport, crewaiImport, pydanticImport bool) models.ToolKind {
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
		// Pydantic AI context tools use ATTRIBUTE decorators on the agent var:
		// `@agent.tool` (takes a leading RunContext) and `@agent.tool_plain` (no
		// ctx). The callee is dotted (`<agentvar>.tool`), and `tool`/`tool_plain`
		// as the attribute suffix is Pydantic's shape. This must route to
		// KindPydanticAITool ONLY when the file imports pydantic_ai and does NOT
		// import the Claude SDK — the `&& !claudeImport` guard is load-bearing:
		// the Claude SDK also exposes an `@agent.tool`, so a Claude-only file (and
		// a file importing BOTH) must fall through to the `callee == "agent.tool"`
		// switch case below, which keeps it KindClaudeSDKTool (claude wins).
		if strings.Contains(callee, ".") && (last == "tool" || last == "tool_plain") &&
			pydanticImport && !claudeImport {
			return models.KindPydanticAITool
		}
		// Precise resolution first: an UNQUALIFIED decorator name that was bound by
		// an explicit import resolves to that import's SDK. This is the exact
		// signal for the otherwise-ambiguous `@tool` (shared by the Claude SDK and
		// LangChain) and works regardless of how many SDKs the file imports. Only
		// bare names are resolved this way; qualified callees (`server.tool`,
		// `agent.tool`) keep their dedicated handling in the switch below.
		if !strings.Contains(callee, ".") {
			if sdk, ok := toolImports[callee]; ok {
				return sdk
			}
		}
		switch {
		// OpenAI Agents SDK — `@function_tool` / `@function_tool(...)`, bare or
		// module-qualified (`agents.function_tool`).
		case callee == "function_tool" || last == "function_tool":
			return models.KindOpenAITool
		// Claude Agent SDK conventions. Real names are still in flux — CSDK is
		// pre-1.0. Expand this list as the SDK stabilizes.
		case callee == "tool":
			// Bare @tool that no import resolved (a star-import or a locally
			// defined `tool`): fall back to file-level import presence. A file
			// that imports exactly one of the @tool-exporting SDKs routes there;
			// the historical Claude default holds when none — or more than one —
			// are present. The extra !crewaiImport / !lcImport guards are no-ops
			// for existing repos (those flags were always false before CrewAI),
			// so this only adds the new CrewAI arm without changing prior routing.
			if lcImport && !claudeImport && !crewaiImport {
				return models.KindLangChainTool
			}
			if crewaiImport && !claudeImport && !lcImport {
				return models.KindCrewAITool
			}
			return models.KindClaudeSDKTool
		case callee == "claude_tool" || last == "claude_tool",
			callee == "agent.tool",
			strings.HasPrefix(callee, "claude_agent_sdk."):
			return models.KindClaudeSDKTool
		// MCP registrations.
		case callee == "server.tool" || callee == "mcp.tool",
			last == "register_tool":
			return models.KindMCPTool
		}
	}
	return models.KindUnknown
}

// trackedToolDecoratorNames are the unqualified tool-decorator names whose owning
// SDK cannot be told from the name alone: `tool` is exported by both the Claude
// Agent SDK and LangChain, so it MUST be resolved by import binding.
// `claude_tool` is Claude-only but tracked so an aliased import still resolves.
var trackedToolDecoratorNames = map[string]bool{"tool": true, "claude_tool": true}

// collectToolImports maps a file's locally-bound tool-decorator names to the SDK
// that exported them, by inspecting `from <module> import <name> [as <alias>]`
// statements. Only the ambiguous names in trackedToolDecoratorNames are recorded.
// When the same name is imported from two SDKs in one file, the last import wins
// — matching Python's runtime binding. Used by kindFromDecorators to disambiguate
// `@tool` between the Claude SDK and LangChain regardless of what else the file
// imports.
func collectToolImports(pf ParsedFile) map[string]models.ToolKind {
	out := map[string]models.ToolKind{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "import_from_statement" {
			return true
		}
		module := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
		var sdk models.ToolKind
		switch {
		case isLangChainModule(module):
			sdk = models.KindLangChainTool
		case isCrewAIModule(module):
			sdk = models.KindCrewAITool
		case module == "claude_agent_sdk" || strings.HasPrefix(module, "claude_agent_sdk."):
			sdk = models.KindClaudeSDKTool
		default:
			return true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(i)
			switch c.Type() {
			case "dotted_name":
				if name := astutil.NodeText(c, pf.Source); trackedToolDecoratorNames[name] {
					out[name] = sdk
				}
			case "aliased_import":
				imported := astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
				alias := astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)
				if trackedToolDecoratorNames[imported] && alias != "" {
					out[alias] = sdk
				}
			}
		}
		return true
	})
	return out
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
	var fileRoot *sitter.Node
	if pf.Tree != nil {
		fileRoot = pf.Tree.RootNode()
	}
	if pythonBodyShellsOut(fn, pf.Source, CollectShellModuleAliases(fileRoot, pf.Source)) {
		facts["shells_out"] = "true"
	}

	// Stage 2 typed captures: static HTTP hosts, static write-path literals,
	// retry presence (tenacity/backoff decorators, client retry kwargs).
	hosts, writePaths, methods, httpCalls, retry := pythonBodyCaptures(fn, pf.Source, fileRoot)
	if retry {
		facts["retry_present"] = "true"
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
		HTTPHosts:      hosts,
		FSWritePaths:   writePaths,
		HTTPMethods:    methods,
		HTTPCalls:      httpCalls,
	}
}

// pythonBodyShellsOut reports whether the function body invokes an OS shell
// primitive (subprocess.*, os.system, os.popen, os.spawn*). Shares the callee
// test (IsShellCallee) and import-alias resolution (aliases.Canonical) with
// rules.PredHasShellCall so a tool's shells_out fact and the has_shell_call
// predicate agree even when the shell module is imported under an alias.
func pythonBodyShellsOut(fn *sitter.Node, src []byte, aliases ShellModuleAliases) bool {
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
		if IsShellCallee(aliases.Canonical(astutil.NodeText(callee, src))) {
			found = true
			return false
		}
		return true
	})
	return found
}
