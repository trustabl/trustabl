package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// pythonBodyCaptures walks a Python tool body and extracts the Stage 2 typed
// captures: static HTTP hosts, static filesystem-write path literals, and a
// best-effort retry-presence signal. Static literals only — any interpolation
// (f-string with substitutions, concatenation, name reference) captures
// nothing, leaving the existing dynamic-URL behavior untouched.
func pythonBodyCaptures(fn *sitter.Node, src []byte, fileRoot *sitter.Node) (hosts, writePaths, methods []string, calls []models.HTTPCall, retry bool) {
	if fn == nil {
		return nil, nil, nil, nil, false
	}
	// fileRoot lets HTTP host capture see module import aliases (import requests
	// as rq) in addition to same-function client aliases (s = requests.Session()).
	aliases := HTTPCallAliases(fileRoot, fn, src)
	hostSet := map[string]bool{}
	pathSet := map[string]bool{}
	methodSet := map[string]bool{}
	callSet := map[string]models.HTTPCall{}

	astutil.Walk(fn, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := n.ChildByFieldName("function")
		if callee == nil {
			return true
		}
		calleeText := astutil.NodeText(callee, src)

		// Recognized HTTP call: capture the host of a string-literal URL arg,
		// and note retry-configuring kwargs (best-effort: requests/httpx
		// spell client-level retries as retries=/max_retries=).
		if canonical, ok := IsHTTPCallNode(n, src, aliases); ok {
			method := httpMethodFromPyCall(canonical, n, src)
			if lit, ok := pythonStringLiteral(firstPositionalArg(n), src); ok {
				if hp, path, ok := hostPathFromURLLiteral(lit); ok {
					hostSet[hp] = true
					if method != "" {
						c := models.HTTPCall{HostPort: hp, Method: method, Path: path}
						callSet[httpCallKey(c)] = c
					}
				}
			}
			if method != "" {
				methodSet[method] = true
			}
			if pythonCallHasKwarg(n, src, "retries") || pythonCallHasKwarg(n, src, "max_retries") {
				retry = true
			}
			return true
		}

		// Recognized write shapes.
		switch {
		case calleeText == "open":
			if pythonOpenIsWrite(n, src) {
				if lit, ok := pythonStringLiteral(firstPositionalArg(n), src); ok {
					pathSet[lit] = true
				}
			}
		case strings.HasSuffix(calleeText, ".write_text") || strings.HasSuffix(calleeText, ".write_bytes"):
			// pathlib.Path("x").write_text(...): the path literal lives on the
			// receiver's constructor call. A computed receiver (Path(a) / b,
			// a variable) captures nothing.
			if lit, ok := pathConstructorLiteral(callee, src); ok {
				pathSet[lit] = true
			}
		case calleeText == "shutil.copy" || calleeText == "shutil.copy2" ||
			calleeText == "shutil.copyfile" || calleeText == "shutil.move":
			// The write target is the second positional argument.
			if lit, ok := pythonStringLiteral(nthPositionalArg(n, 1), src); ok {
				pathSet[lit] = true
			}
		}
		return true
	})

	if pythonHasRetryDecorator(fn, src) {
		retry = true
	}
	return setToSorted(hostSet), setToSorted(pathSet), setToSorted(methodSet), sortedHTTPCalls(callSet), retry
}

// httpMethodFromPyCall derives the uppercase HTTP verb of a recognized Python
// HTTP call from its canonical callee. A verb-named call (requests.post,
// httpx.get, aiohttp.ClientSession.get) carries the verb in its last segment;
// requests.request/httpx.request carry it as the first positional string arg
// or a method= kwarg; urllib's urlopen defaults to GET. Returns "" when the
// method is not statically provable (e.g. request(method_var, ...)).
func httpMethodFromPyCall(canonical string, call *sitter.Node, src []byte) string {
	seg := canonical
	if i := strings.LastIndexByte(canonical, '.'); i >= 0 {
		seg = canonical[i+1:]
	}
	switch strings.ToLower(seg) {
	case "get", "post", "put", "patch", "delete", "head", "options":
		return strings.ToUpper(seg)
	case "request":
		if lit, ok := pythonStringLiteral(firstPositionalArg(call), src); ok {
			return strings.ToUpper(lit)
		}
		if lit, ok := pythonKwargStringLiteral(call, src, "method"); ok {
			return strings.ToUpper(lit)
		}
	case "urlopen":
		return "GET"
	}
	return ""
}

// pythonKwargStringLiteral returns the static string value of a named keyword
// argument, or ("", false) when absent or non-literal.
func pythonKwargStringLiteral(call *sitter.Node, src []byte, name string) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		if k := arg.ChildByFieldName("name"); k != nil && astutil.NodeText(k, src) == name {
			return pythonStringLiteral(arg.ChildByFieldName("value"), src)
		}
	}
	return "", false
}

// firstPositionalArg returns a call's first non-keyword argument node.
func firstPositionalArg(call *sitter.Node) *sitter.Node {
	return nthPositionalArg(call, 0)
}

// nthPositionalArg returns a call's nth (0-based) non-keyword argument node.
func nthPositionalArg(call *sitter.Node, n int) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	seen := 0
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() == "keyword_argument" {
			continue
		}
		if seen == n {
			return arg
		}
		seen++
	}
	return nil
}

// pythonStringLiteral returns the unquoted text of a static Python string
// literal node. F-strings carrying interpolation children are rejected (an
// f-string with no substitutions is effectively a literal and is accepted);
// concatenations, name refs, and every other node type are rejected.
func pythonStringLiteral(n *sitter.Node, src []byte) (string, bool) {
	if n == nil || n.Type() != "string" {
		return "", false
	}
	dynamic := false
	astutil.Walk(n, func(c *sitter.Node) bool {
		if c.Type() == "interpolation" {
			dynamic = true
			return false
		}
		return true
	})
	if dynamic {
		return "", false
	}
	raw := astutil.NodeText(n, src)
	// Strip any prefix letters (r"", b"", f"", rb"" ...) down to the first
	// quote, then the quotes themselves (Trim handles triple quotes too).
	if i := strings.IndexAny(raw, `"'`); i > 0 {
		raw = raw[i:]
	}
	v := strings.Trim(raw, `"'`)
	if v == "" {
		return "", false
	}
	return v, true
}

// pythonCallHasKwarg reports whether a call passes the named keyword argument.
func pythonCallHasKwarg(call *sitter.Node, src []byte, name string) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		if k := arg.ChildByFieldName("name"); k != nil && astutil.NodeText(k, src) == name {
			return true
		}
	}
	return false
}

// pythonOpenIsWrite reports whether an open(...) call opens for writing: a
// literal mode (second positional or mode=) containing w/a/x/+. A dynamic
// mode is conservatively NOT a write capture.
func pythonOpenIsWrite(call *sitter.Node, src []byte) bool {
	mode := nthPositionalArg(call, 1)
	if mode == nil {
		args := call.ChildByFieldName("arguments")
		if args == nil {
			return false
		}
		for i := 0; i < int(args.NamedChildCount()); i++ {
			arg := args.NamedChild(i)
			if arg == nil || arg.Type() != "keyword_argument" {
				continue
			}
			if k := arg.ChildByFieldName("name"); k != nil && astutil.NodeText(k, src) == "mode" {
				mode = arg.ChildByFieldName("value")
				break
			}
		}
	}
	lit, ok := pythonStringLiteral(mode, src)
	if !ok {
		return false
	}
	return strings.ContainsAny(lit, "wax+")
}

// pathConstructorLiteral extracts the path literal from a
// Path("...")/pathlib.Path("...") receiver of a .write_text/.write_bytes
// attribute callee.
func pathConstructorLiteral(callee *sitter.Node, src []byte) (string, bool) {
	if callee == nil || callee.Type() != "attribute" {
		return "", false
	}
	obj := callee.ChildByFieldName("object")
	if obj == nil || obj.Type() != "call" {
		return "", false
	}
	inner := obj.ChildByFieldName("function")
	if inner == nil {
		return "", false
	}
	switch astutil.NodeText(inner, src) {
	case "Path", "pathlib.Path", "PurePath", "pathlib.PurePath":
		return pythonStringLiteral(firstPositionalArg(obj), src)
	}
	return "", false
}

// pythonHasRetryDecorator reports whether the function carries a recognized
// retry decorator: tenacity's @retry / @tenacity.retry or any @backoff.*
// form. Best-effort by decorator text — a same-named local decorator counts,
// which is the honest trade for not chasing import bindings here.
func pythonHasRetryDecorator(fn *sitter.Node, src []byte) bool {
	parent := fn.Parent()
	if parent == nil || parent.Type() != "decorated_definition" {
		return false
	}
	for i := 0; i < int(parent.NamedChildCount()); i++ {
		dec := parent.NamedChild(i)
		if dec == nil || dec.Type() != "decorator" {
			continue
		}
		text := strings.TrimPrefix(astutil.NodeText(dec, src), "@")
		if strings.HasPrefix(text, "retry") || strings.HasPrefix(text, "tenacity.retry") ||
			strings.HasPrefix(text, "backoff.") {
			return true
		}
	}
	return false
}
