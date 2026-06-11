package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// handlerCapture is everything one walk of a TS/JS handler body extracts:
// the boolean body facts plus the Stage 2 typed captures (static HTTP hosts,
// write-path literals, and HTTP method verbs).
type handlerCapture struct {
	facts        map[string]string
	httpHosts    []string
	fsWritePaths []string
	httpMethods  []string
}

// tsHandlerCapture walks a handler node (arrow_function or function) and
// returns body facts plus typed captures. Recognizes JS/TS shell and HTTP
// call shapes used by both Claude SDK tool() handlers and OpenAI Agents SDK
// tool({execute: ...}) handlers. Lifted from ts_discovery.go so every TS
// discovery path shares it.
func tsHandlerCapture(handler *sitter.Node, src []byte) handlerCapture {
	out := handlerCapture{facts: map[string]string{}}
	if handler == nil {
		return out
	}
	hostSet := map[string]bool{}
	pathSet := map[string]bool{}
	methodSet := map[string]bool{}
	astutil.Walk(handler, func(n *sitter.Node) bool {
		// new_expression: `new Function(...)` is NOT a call_expression; its
		// constructor identifier is at ChildByFieldName("constructor").
		// Verified tree-sitter node type: new_expression, field "constructor"
		// returns the identifier node (text "Function").
		if n.Type() == "new_expression" {
			ctor := n.ChildByFieldName("constructor")
			if ctor != nil && astutil.NodeText(ctor, src) == "Function" {
				out.facts["code_exec"] = "true"
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
			out.facts["http_call"] = "true"
			if urlArgIsDynamic(n, src) {
				out.facts["dynamic_url"] = "true"
			} else if lit, ok := tsStringLiteral(firstCallArg(n), src); ok {
				// Stage 2: the non-dynamic branch records the literal URL's
				// canonical host:port. A relative literal has no host and
				// captures nothing.
				if hp, ok := hostFromURLLiteral(lit); ok {
					hostSet[hp] = true
				}
			}
			if m := tsHTTPMethod(text, n, src); m != "" {
				methodSet[m] = true
			}
			if !httpCallHasTimeout(n, src) {
				out.facts["http_no_timeout"] = "true"
			}
			// got-style retry: an options object carrying a `retry` key.
			if httpCallHasOptionKey(n, src, "retry") {
				out.facts["retry_present"] = "true"
			}
		case "execSync", "exec", "execFile", "execFileSync", "spawn", "spawnSync", "fork",
			// Namespace-import / require shape: `child_process.exec(...)` from
			// `import * as child_process` or `const child_process = require(...)`.
			// The bare cases above catch the destructured `const { exec } = ...`.
			"child_process.exec", "child_process.execSync",
			"child_process.spawn", "child_process.spawnSync",
			"child_process.execFile", "child_process.execFileSync",
			"child_process.fork":
			out.facts["shells_out"] = "true"
		case "writeFile", "writeFileSync", "appendFile", "appendFileSync",
			"createWriteStream",
			// Namespace-import shape: `fs.writeFileSync(...)` /
			// `fsPromises.writeFile(...)`. The bare cases above catch the
			// destructured `const { writeFileSync } = require("fs")` form.
			"fs.writeFile", "fs.writeFileSync", "fs.appendFile",
			"fs.appendFileSync", "fs.createWriteStream",
			"fsPromises.writeFile", "fsPromises.appendFile":
			out.facts["writes_fs"] = "true"
			// Stage 2: capture a literal first-arg path. A joined/computed
			// path (path.join(...), template substitution) captures nothing.
			if lit, ok := tsStringLiteral(firstCallArg(n), src); ok {
				pathSet[lit] = true
			}
		case "eval":
			// Bare `eval` callee only — callee text for `retrieval(x)` is
			// "retrieval", so this exact-match eliminates the false-positive.
			out.facts["code_exec"] = "true"
		case "pRetry", "axiosRetry", "backOff", "retry":
			// Retry wrappers: p-retry's default import (pRetry), axios-retry
			// (axiosRetry), exponential-backoff (backOff), and async-retry's
			// conventional `retry` import. Best-effort by callee text — a
			// same-named local helper counts, the honest trade for not
			// chasing import bindings here.
			out.facts["retry_present"] = "true"
		}
		return true
	})
	out.httpHosts = setToSorted(hostSet)
	out.fsWritePaths = setToSorted(pathSet)
	out.httpMethods = setToSorted(methodSet)
	return out
}

// tsHTTPMethod derives the uppercase HTTP verb of a recognized TS/JS HTTP call.
// A verb-named call (axios.post, got.get) carries the verb in its last segment;
// fetch / axios(config) / axios.request / got / undici.* carry it as a `method:`
// option, defaulting to GET (the fetch/get-style default) when absent.
func tsHTTPMethod(callee string, call *sitter.Node, src []byte) string {
	seg := callee
	if i := strings.LastIndexByte(callee, '.'); i >= 0 {
		seg = callee[i+1:]
	}
	switch strings.ToLower(seg) {
	case "get", "post", "put", "patch", "delete", "head":
		return strings.ToUpper(seg)
	}
	if m, ok := httpCallOptionStringValue(call, src, "method"); ok {
		return strings.ToUpper(m)
	}
	return "GET"
}

// httpCallOptionStringValue returns the static string value of a named
// top-level key in any options-object argument of the call, or ("", false).
func httpCallOptionStringValue(call *sitter.Node, src []byte, key string) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() != "object" {
			continue
		}
		for j := 0; j < int(arg.NamedChildCount()); j++ {
			prop := arg.NamedChild(j)
			if prop == nil || prop.Type() != "pair" {
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
			if kname == key {
				return tsStringLiteral(prop.ChildByFieldName("value"), src)
			}
		}
	}
	return "", false
}

// firstCallArg returns the first positional argument node of a TS call.
func firstCallArg(call *sitter.Node) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return nil
	}
	return args.NamedChild(0)
}

// tsStringLiteral returns the unquoted text of a static TS string literal: a
// plain "..."/'...' string, or a backtick template with zero substitutions
// (effectively a literal). Everything else — template with ${...},
// identifier, member access, concatenation, call — is dynamic and captures
// nothing.
func tsStringLiteral(n *sitter.Node, src []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "string":
		raw := astutil.NodeText(n, src)
		if len(raw) < 2 {
			return "", false
		}
		return raw[1 : len(raw)-1], true
	case "template_string":
		if n.NamedChildCount() > 0 {
			return "", false
		}
		raw := astutil.NodeText(n, src)
		if len(raw) < 2 {
			return "", false
		}
		return raw[1 : len(raw)-1], true
	}
	return "", false
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
	arg := firstCallArg(call)
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
	for k := range timeoutOptionKeys {
		if httpCallHasOptionKey(call, src, k) {
			return true
		}
	}
	return false
}

// httpCallHasOptionKey reports whether any options-object argument of the
// call carries the named top-level key.
func httpCallHasOptionKey(call *sitter.Node, src []byte, key string) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() != "object" {
			continue
		}
		for j := 0; j < int(arg.NamedChildCount()); j++ {
			prop := arg.NamedChild(j)
			if prop == nil || prop.Type() != "pair" {
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
			if kname == key {
				return true
			}
		}
	}
	return false
}
