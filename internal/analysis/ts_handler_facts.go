package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// tsHandlerFacts walks a handler node (arrow_function or function) and
// returns body facts. Recognizes JS/TS shell and HTTP call shapes used by
// both Claude SDK tool() handlers and OpenAI Agents SDK tool({execute: ...})
// handlers. Lifted from ts_discovery.go so both discovery paths share it.
func tsHandlerFacts(handler *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	if handler == nil {
		return out
	}
	astutil.Walk(handler, func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		text := astutil.NodeText(fn, src)
		switch text {
		case "fetch", "axios", "axios.get", "axios.post", "axios.put", "axios.delete",
			"axios.patch", "axios.request", "got", "got.get", "got.post",
			"undici.fetch", "undici.request":
			out["http_call"] = "true"
			if urlArgIsDynamic(n, src) {
				out["dynamic_url"] = "true"
			}
		case "execSync", "exec", "spawn", "spawnSync", "fork",
			// Namespace-import / require shape: `child_process.exec(...)` from
			// `import * as child_process` or `const child_process = require(...)`.
			// The bare cases above catch the destructured `const { exec } = ...`.
			"child_process.exec", "child_process.execSync",
			"child_process.spawn", "child_process.spawnSync",
			"child_process.execFile", "child_process.execFileSync",
			"child_process.fork":
			out["shells_out"] = "true"
		}
		return true
	})
	return out
}

// urlArgIsDynamic reports whether the first positional argument of an HTTP
// call node is something other than a plain string literal — i.e. a template
// string with substitutions, an identifier, a member expression, or a string
// concatenation. Those are caller/model-controlled URLs (the SSRF signal).
//
// Verified tree-sitter node types (typescript grammar):
//   - Plain string (`"..."` or `'...'`): type "string".
//   - Template string (`` `...` ``): type "template_string"; backtick delimiters
//     are anonymous children, so a plain backtick template with no ${...} has
//     NamedChildCount() == 0, while one with substitutions has at least one
//     "template_substitution" named child (NamedChildCount() > 0).
//   - Identifier: type "identifier".
//   - Member expression: type "member_expression".
//   - Binary expression (concat): type "binary_expression".
//   - Call expression: type "call_expression".
//
// The `arguments` field name is confirmed — call_expression uses ChildByFieldName("arguments")
// throughout this package (see extractTSOpenAITool, extractTSADKTool).
func urlArgIsDynamic(call *sitter.Node, src []byte) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	var arg *sitter.Node
	if args.NamedChildCount() > 0 {
		arg = args.NamedChild(0)
	}
	if arg == nil {
		return false
	}
	switch arg.Type() {
	case "string":
		return false // plain literal URL — safe
	case "template_string":
		// A template with no ${...} substitution is effectively a literal.
		// The backtick characters are anonymous (unnamed) children, so
		// NamedChildCount() > 0 means there is at least one template_substitution.
		return arg.NamedChildCount() > 0
	default:
		// identifier, member_expression, binary_expression (concat), call, etc.
		return true
	}
}
