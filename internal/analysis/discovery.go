// Package analysis implements Tool Discovery + Detector Suite + Scoring
// (architecture §2).
package analysis

import (
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/karenctl/internal/analysis/astutil"
	"github.com/trustabl/karenctl/internal/models"
)

// ParsedFile pairs a file's source bytes with its tree-sitter root.
// We hand this to detectors so they don't re-parse.
type ParsedFile struct {
	RelPath string
	Source  []byte
	Tree    *sitter.Tree
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
func DiscoverTools(manifest models.ScanManifest) ([]models.ToolDef, []ParsedFile, error) {
	var tools []models.ToolDef
	var parsed []ParsedFile

	for _, rel := range manifest.PythonFiles {
		abs := filepath.Join(manifest.RepoRoot, rel)
		src, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		tree, err := astutil.Parse(src)
		if err != nil {
			// One unparseable file shouldn't fail the scan. Surface upstream
			// via the result if needed; for now, skip silently.
			continue
		}
		pf := ParsedFile{RelPath: rel, Source: src, Tree: tree}
		parsed = append(parsed, pf)
		tools = append(tools, toolsInFile(pf)...)
	}
	return tools, parsed, nil
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
		out = append(out, buildTool(fn, pf, kind))
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
		text := astutil.NodeText(d, src)
		lower := strings.ToLower(text)
		switch {
		// OpenAI Agents SDK — `@function_tool` and `@function_tool(...)`.
		case strings.Contains(lower, "@function_tool"):
			return models.KindOpenAITool
		// Claude Agent SDK conventions. Real names are still in flux —
		// CSDK is pre-1.0. Expand this list as the SDK stabilizes.
		case strings.Contains(lower, "@tool"),
			strings.Contains(lower, "@claude_tool"),
			strings.Contains(lower, "@agent.tool"),
			strings.Contains(lower, "claude_agent_sdk"):
			return models.KindClaudeSDKTool
		case strings.Contains(lower, "@server.tool"),
			strings.Contains(lower, "@mcp.tool"),
			strings.Contains(lower, ".register_tool"):
			return models.KindMCPTool
		}
	}
	return models.KindUnknown
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

func buildTool(fn *sitter.Node, pf ParsedFile, kind models.ToolKind) models.ToolDef {
	name := astutil.FunctionName(fn, pf.Source)
	params := astutil.FunctionParams(fn, pf.Source)
	// Drop `self`/`cls`.
	filtered := params[:0]
	for _, p := range params {
		if p == "self" || p == "cls" {
			continue
		}
		filtered = append(filtered, p)
	}

	return models.ToolDef{
		Name:           name,
		Kind:           kind,
		Language:       models.LanguagePython, // discovery is python-only today; widen when a TS parser lands
		FilePath:       pf.RelPath,
		Line:           astutil.NodeLine(fn),
		EndLine:        astutil.NodeEndLine(fn),
		Description:    astutil.FunctionDocstring(fn, pf.Source),
		HasTypedParams: astutil.FunctionHasTypedParams(fn),
		ParamNames:     filtered,
		Facts:          map[string]string{},
	}
}
