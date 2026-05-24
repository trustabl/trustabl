package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// FindFunctionNode locates the function_definition node for a tool inside its
// parsed file. Detectors and rule predicates carry only positions; storing
// nodes in ToolDef would couple JSON serialization to tree-sitter internals.
func FindFunctionNode(t models.ToolDef, pf ParsedFile) *sitter.Node {
	if pf.Tree == nil {
		return nil
	}
	var match *sitter.Node
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if match != nil {
			return false
		}
		if n.Type() != "function_definition" {
			return true
		}
		if astutil.NodeLine(n) == t.Line && astutil.FunctionName(n, pf.Source) == t.Name {
			match = n
			return false
		}
		return true
	})
	return match
}

// IsHTTPCall returns true if the literal callee text matches a known HTTP
// client function. This is the direct-call check; aliased session calls
// (e.g. `s.get(...)` where `s = requests.Session()`) are resolved by
// IsHTTPCallNode, which delegates here for the direct case.
func IsHTTPCall(callee string) bool {
	switch callee {
	case "requests.get", "requests.post", "requests.put", "requests.delete",
		"requests.patch", "requests.head", "requests.request",
		"requests.Session.get", "requests.Session.post",
		"httpx.get", "httpx.post", "httpx.put", "httpx.delete",
		"httpx.patch", "httpx.head", "httpx.request",
		"httpx.AsyncClient", "httpx.Client",
		"urllib.request.urlopen", "aiohttp.ClientSession.get",
		"aiohttp.ClientSession.post":
		return true
	}
	return false
}

// clientConstructorModule returns the canonical HTTP client module for a
// constructor callee text (e.g. "requests.Session" -> "requests"), or "" if
// the callee is not a recognized client constructor. Single source of truth
// for the recognized client set.
func clientConstructorModule(calleeText string) string {
	switch calleeText {
	// requests.session (lowercase) is the library's legacy factory function,
	// equivalent to requests.Session(); both are real and in use.
	case "requests.Session", "requests.session":
		return "requests"
	case "httpx.Client", "httpx.AsyncClient":
		return "httpx"
	case "aiohttp.ClientSession":
		return "aiohttp"
	}
	return ""
}

// ResolveClientAliases walks a function body and returns a map of
// local-variable name -> canonical HTTP client module ("requests", "httpx",
// "aiohttp") for variables bound to a recognized client constructor, via
// either assignment (s = requests.Session()) or a with-binding
// (with httpx.Client() as c:). Same-function scope only — instance attributes
// (self.client) and cross-function/module aliases are intentionally NOT
// resolved. Best-effort heuristic: last write wins on reassignment.
func ResolveClientAliases(fn *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	if fn == nil {
		return out
	}
	astutil.Walk(fn, func(n *sitter.Node) bool {
		switch n.Type() {
		case "assignment":
			left := n.ChildByFieldName("left")
			right := n.ChildByFieldName("right")
			if left == nil || left.Type() != "identifier" {
				return true
			}
			name := astutil.NodeText(left, src)
			if right == nil || right.Type() != "call" {
				delete(out, name) // rebound to a non-call: clear any prior alias
				return true
			}
			callee := right.ChildByFieldName("function")
			if callee == nil {
				delete(out, name)
				return true
			}
			if mod := clientConstructorModule(astutil.NodeText(callee, src)); mod != "" {
				out[name] = mod
			} else {
				delete(out, name) // rebound to a non-client call
			}
		case "with_statement":
			resolveWithAliases(n, src, out)
		}
		return true
	})
	return out
}

// resolveWithAliases handles `with <client constructor>() as name:` bindings,
// mirroring the traversal in collectWithStatementMCPAliases (mcp_servers.go).
func resolveWithAliases(with *sitter.Node, src []byte, out map[string]string) {
	for i := 0; i < int(with.NamedChildCount()); i++ {
		clause := with.NamedChild(i)
		if clause.Type() != "with_clause" {
			continue
		}
		for j := 0; j < int(clause.NamedChildCount()); j++ {
			item := clause.NamedChild(j)
			if item.Type() != "with_item" {
				continue
			}
			value := item.ChildByFieldName("value")
			if value == nil || value.Type() != "as_pattern" {
				continue
			}
			callNode := value.NamedChild(0)
			aliasField := value.ChildByFieldName("alias")
			if callNode == nil || aliasField == nil || callNode.Type() != "call" {
				continue
			}
			aliasNode := aliasField
			if aliasField.NamedChildCount() > 0 {
				aliasNode = aliasField.NamedChild(0)
			}
			callee := callNode.ChildByFieldName("function")
			if callee == nil {
				continue
			}
			if mod := clientConstructorModule(astutil.NodeText(callee, src)); mod != "" {
				out[astutil.NodeText(aliasNode, src)] = mod
			}
		}
	}
}

// IsHTTPCallNode resolves a call node to its canonical HTTP callee, handling
// both direct calls (requests.get -> "requests.get") and aliased attribute
// calls (s.get where aliases["s"]=="requests" -> "requests.get"). Returns the
// canonical callee text and true if it is a recognized HTTP call. The
// canonical string is what rule `callees` lists are written against
// (requests.get, httpx.post, ...).
func IsHTTPCallNode(call *sitter.Node, src []byte, aliases map[string]string) (string, bool) {
	if call == nil {
		return "", false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", false
	}
	calleeText := astutil.NodeText(fn, src)

	// Direct call (requests.get, httpx.post, urllib.request.urlopen, ...).
	if IsHTTPCall(calleeText) {
		return calleeText, true
	}

	// Aliased attribute call: <ident>.<method> where <ident> is a known client
	// alias. Canonicalize to <module>.<method>.
	if fn.Type() == "attribute" {
		obj := fn.ChildByFieldName("object")
		attr := fn.ChildByFieldName("attribute")
		if obj != nil && obj.Type() == "identifier" && attr != nil {
			if mod, ok := aliases[astutil.NodeText(obj, src)]; ok {
				return mod + "." + astutil.NodeText(attr, src), true
			}
		}
	}
	return "", false
}

// IsPathishParam returns true if a parameter name is clearly path-like.
// Uses word-boundary logic to avoid matching names like "editor_id" or
// "directory_service_url" that merely contain the substring.
func IsPathishParam(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "path", "file", "filename", "filepath", "dir", "directory":
		return true
	}
	return strings.HasSuffix(lower, "_path") ||
		strings.HasSuffix(lower, "_file") ||
		strings.HasSuffix(lower, "_dir") ||
		strings.HasSuffix(lower, "_directory") ||
		strings.HasPrefix(lower, "file_") ||
		strings.HasPrefix(lower, "path_")
}
