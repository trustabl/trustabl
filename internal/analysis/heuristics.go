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
//
// Matching is primarily by START LINE, which uniquely identifies a
// function_definition (two defs cannot begin on the same line). The tool Name is
// used only as a tie-break/confirmation when it matches the def's name; when a
// tool was registered under a name= override that differs from the function name
// (e.g. AutoGen register_function(fn, name="x") or a LangChain factory name=),
// the line still resolves the body so the body-scan predicates (has_shell_call,
// has_code_exec_call, has_dynamic_url_call, call_without_kwarg) keep working. A
// line-only fallback cannot misfire: the line is unique, and a non-zero
// disagreement on name does not point at a different function.
func FindFunctionNode(t models.ToolDef, pf ParsedFile) *sitter.Node {
	if pf.Tree == nil {
		return nil
	}
	var lineMatch *sitter.Node
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "function_definition" {
			return true
		}
		if astutil.NodeLine(n) != t.Line {
			return true
		}
		// Same line: an exact name match is the confident hit; return it. A
		// name mismatch (name= override) still resolves via the line, recorded
		// as a fallback in case no exact-name def shares this line.
		if astutil.FunctionName(n, pf.Source) == t.Name {
			lineMatch = n
			return false
		}
		if lineMatch == nil {
			lineMatch = n
		}
		return true
	})
	return lineMatch
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

// clientConstructorModule returns the canonical receiver prefix for an HTTP
// client constructor callee text, or "" if the callee is not a recognized
// client constructor. Single source of truth for the recognized client set.
//
// The return value is the prefix that IsHTTPCallNode prepends to an aliased
// method call (alias.get -> "<prefix>.get"), so it MUST be the prefix the rule
// callee lists are written against. For requests/httpx the module name is the
// receiver prefix (requests.get, httpx.get). For aiohttp the call surface lives
// on ClientSession, so the prefix is "aiohttp.ClientSession" — yielding
// aiohttp.ClientSession.get, which is what IsHTTPCall recognizes. Returning a
// bare "aiohttp" would produce the unmatchable "aiohttp.get".
func clientConstructorModule(calleeText string) string {
	switch calleeText {
	// requests.session (lowercase) is the library's legacy factory function,
	// equivalent to requests.Session(); both are real and in use.
	case "requests.Session", "requests.session":
		return "requests"
	case "httpx.Client", "httpx.AsyncClient":
		return "httpx"
	case "aiohttp.ClientSession":
		return "aiohttp.ClientSession"
	}
	return ""
}

// ResolveClientAliases walks a function body and returns a map of
// local-variable name -> canonical receiver prefix ("requests", "httpx",
// "aiohttp.ClientSession") for variables bound to a recognized client
// constructor, via either assignment (s = requests.Session()) or a with-binding
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

// ShellModuleAliases canonicalizes aliased references to the shell-capable
// Python modules (subprocess, os) back to their real dotted callee, so an
// import alias cannot silently evade shell-call detection. Built from a
// parsed file's imports:
//
//	import subprocess as sp          -> module["sp"]  = "subprocess"
//	import os as o                   -> module["o"]   = "os"
//	from subprocess import run       -> symbol["run"] = "subprocess.run"
//	from subprocess import run as r  -> symbol["r"]   = "subprocess.run"
//
// Only subprocess/os are tracked (the modules IsShellCallee cares about), so
// the maps stay tiny. Plain `import subprocess` needs no entry — its calls
// already read `subprocess.run` literally.
type ShellModuleAliases struct {
	module map[string]string // local module alias -> "subprocess" | "os"
	symbol map[string]string // local symbol name  -> e.g. "subprocess.run"
}

var shellAliasModules = map[string]bool{"subprocess": true, "os": true}

// CollectShellModuleAliases scans a parsed Python file (whole tree, so both
// module-level and function-local imports are seen) for subprocess/os import
// aliases. Parsing is text-based over the import statement nodes — robust to
// tree-sitter field-name drift, and only the two relevant modules are kept.
func CollectShellModuleAliases(root *sitter.Node, src []byte) ShellModuleAliases {
	a := ShellModuleAliases{module: map[string]string{}, symbol: map[string]string{}}
	if root == nil {
		return a
	}
	norm := func(s string) string {
		s = strings.NewReplacer("(", " ", ")", " ", "\n", " ", "\t", " ", "\\", " ").Replace(s)
		return strings.Join(strings.Fields(s), " ")
	}
	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_statement":
			// import a [as x], b [as y]
			rest := strings.TrimSpace(strings.TrimPrefix(norm(astutil.NodeText(n, src)), "import "))
			for _, part := range strings.Split(rest, ",") {
				part = strings.TrimSpace(part)
				if mod, alias, ok := splitAs(part); ok {
					if shellAliasModules[mod] {
						a.module[alias] = mod
					}
				}
			}
		case "import_from_statement":
			// from MODULE import a [as x], b [as y]
			t := norm(astutil.NodeText(n, src))
			t = strings.TrimSpace(strings.TrimPrefix(t, "from "))
			i := strings.Index(t, " import ")
			if i < 0 {
				return true
			}
			mod := strings.TrimSpace(t[:i])
			if !shellAliasModules[mod] {
				return true
			}
			for _, part := range strings.Split(t[i+len(" import "):], ",") {
				part = strings.TrimSpace(part)
				if part == "" || part == "*" {
					continue
				}
				if orig, alias, ok := splitAs(part); ok {
					a.symbol[alias] = mod + "." + orig
				} else {
					a.symbol[part] = mod + "." + part
				}
			}
		}
		return true
	})
	return a
}

// splitAs parses an "X as Y" import clause, returning ("X","Y",true), or the
// bare name with ok=false when there is no alias.
func splitAs(part string) (name, alias string, ok bool) {
	fields := strings.Fields(part)
	if len(fields) == 3 && fields[1] == "as" {
		return fields[0], fields[2], true
	}
	return part, "", false
}

// Canonical rewrites a callee ("sp.run", "run", "subprocess.run") to its real
// dotted form using the collected aliases. Unmapped callees pass through
// unchanged, so a literal `subprocess.run` still works with empty aliases.
func (a ShellModuleAliases) Canonical(callee string) string {
	if dot := strings.IndexByte(callee, '.'); dot >= 0 {
		if real, ok := a.module[callee[:dot]]; ok {
			return real + callee[dot:]
		}
		return callee
	}
	if real, ok := a.symbol[callee]; ok {
		return real
	}
	return callee
}

// IsShellCallee reports whether a (canonicalized) callee names an OS shell
// primitive: subprocess.*, os.system, os.popen, or os.spawn*. Shared by the
// discovery shells_out fact and the has_shell_call rule predicate so they
// match identically.
func IsShellCallee(c string) bool {
	return strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" ||
		strings.HasPrefix(c, "os.spawn")
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
