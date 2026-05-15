package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/karenctl/internal/analysis/astutil"
	"github.com/trustabl/karenctl/internal/models"
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
// client function. Limitation: aliased session calls (e.g. `s.get(...)` where
// `s = requests.Session()`) are not resolved — the matcher is exact-text only.
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
