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
		// new_expression: `new Function(...)` is NOT a call_expression; its
		// constructor identifier is at ChildByFieldName("constructor").
		// Verified tree-sitter node type: new_expression, field "constructor"
		// returns the identifier node (text "Function").
		if n.Type() == "new_expression" {
			ctor := n.ChildByFieldName("constructor")
			if ctor != nil && astutil.NodeText(ctor, src) == "Function" {
				out["code_exec"] = "true"
			}
			return true
		}
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
			if !httpCallHasTimeout(n, src) {
				out["http_no_timeout"] = "true"
			}
		case "execSync", "exec", "execFile", "execFileSync", "spawn", "spawnSync", "fork",
			// Namespace-import / require shape: `child_process.exec(...)` from
			// `import * as child_process` or `const child_process = require(...)`.
			// The bare cases above catch the destructured `const { exec } = ...`.
			"child_process.exec", "child_process.execSync",
			"child_process.spawn", "child_process.spawnSync",
			"child_process.execFile", "child_process.execFileSync",
			"child_process.fork":
			out["shells_out"] = "true"
		case "writeFile", "writeFileSync", "appendFile", "appendFileSync",
			"createWriteStream",
			// Namespace-import shape: `fs.writeFileSync(...)` /
			// `fsPromises.writeFile(...)`. The bare cases above catch the
			// destructured `const { writeFileSync } = require("fs")` form.
			"fs.writeFile", "fs.writeFileSync", "fs.appendFile",
			"fs.appendFileSync", "fs.createWriteStream",
			"fsPromises.writeFile", "fsPromises.appendFile":
			out["writes_fs"] = "true"
		case "eval":
			// Bare `eval` callee only — callee text for `retrieval(x)` is
			// "retrieval", so this exact-match eliminates the false-positive.
			out["code_exec"] = "true"
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
//   - Template string (“ `...` “): type "template_string"; backtick delimiters
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

// timeoutOptionKeys are the option-object property names that bound an HTTP
// call's duration: fetch's `signal` (an AbortSignal), axios/got's `timeout`,
// and the Vercel AI SDK's `abortSignal`. A recognized HTTP call carrying an
// options object with any of these keys has a timeout bound.
var timeoutOptionKeys = map[string]bool{
	"signal":      true,
	"timeout":     true,
	"abortSignal": true,
}

// httpCallHasTimeout reports whether a recognized HTTP call node passes a
// timeout bound — an options-object argument carrying a `signal`, `timeout`, or
// `abortSignal` key. A bare call (no options object) or one whose options omit
// all three is treated as unbounded. This is a heuristic: it cannot see a
// signal/timeout defined on another line and passed by identifier, so the rule
// built on the resulting `http_no_timeout` fact is calibrated at modest
// confidence (and its blind spots are documented in the rulebook).
func httpCallHasTimeout(call *sitter.Node, src []byte) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg.Type() != "object" {
			continue
		}
		for j := 0; j < int(arg.NamedChildCount()); j++ {
			prop := arg.NamedChild(j)
			if prop.Type() != "pair" {
				continue
			}
			k := prop.ChildByFieldName("key")
			if k == nil {
				continue
			}
			var kname string
			switch k.Type() {
			case "property_identifier":
				kname = astutil.NodeText(k, src)
			case "string":
				raw := astutil.NodeText(k, src)
				if len(raw) >= 2 {
					kname = raw[1 : len(raw)-1]
				}
			}
			if timeoutOptionKeys[kname] {
				return true
			}
		}
	}
	return false
}
