package analysis

import (
	"sort"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// MCPServerClasses is the closed set of MCP server classes recognized by
// discovery. Source of truth: openai-agents-python/src/agents/mcp/server.py.
var MCPServerClasses = map[string]string{
	"MCPServerStdio":          "stdio",
	"MCPServerSse":            "sse",
	"MCPServerStreamableHttp": "streamable_http",
}

// IsMCPServerClass reports whether className is a recognized MCP server class.
func IsMCPServerClass(className string) bool {
	_, ok := MCPServerClasses[className]
	return ok
}

// MCPTransportFromClass returns "stdio" / "sse" / "streamable_http", or "" if
// the class is not recognized.
func MCPTransportFromClass(className string) string {
	return MCPServerClasses[className]
}

// classifyMCPServerCall inspects an ExprCall item from an mcp_servers=[...]
// list and returns an MCPServerDef + true if the callee names a known class.
// Mirrors hosted_tools.classifyHostedToolCall.
func classifyMCPServerCall(callItem models.Expr, filePath string, line int) (models.MCPServerDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.MCPServerDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsMCPServerClass(name) {
		return models.MCPServerDef{}, false
	}
	// Kwargs intentionally not captured at v1 — Expr.Text preserves the raw
	// call site for any future detector that needs the args (e.g. inspecting
	// MCPServerStdio params.command). Reparsing the kwargs from the ExprCall
	// text into MCPServerDef.Kwargs is a fast-follow if a rule needs them.
	return models.MCPServerDef{
		Class:     name,
		Transport: MCPTransportFromClass(name),
		SDK:       models.SDKOpenAIAgents,
		FilePath:  filePath,
		Line:      line,
	}, true
}

func sortMCPServers(ms []models.MCPServerDef) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].FilePath != ms[j].FilePath {
			return ms[i].FilePath < ms[j].FilePath
		}
		if ms[i].Line != ms[j].Line {
			return ms[i].Line < ms[j].Line
		}
		return ms[i].Class < ms[j].Class
	})
}

// collectWithStatementMCPAliases walks a parsed file and returns a map from
// alias name → MCPServerDef for every `with` / `async with MCPServer*(...) as
// alias:` statement. Only the Class/Transport/SDK of the returned value are
// authoritative; callers re-attribute FilePath/Line to the using agent.
//
// tree-sitter-python shape for `async with X() as y:`:
//
//	with_statement
//	  └─ with_clause
//	       └─ with_item            (field "value" → as_pattern)
//	            └─ as_pattern
//	                 ├─ call               (named child 0)
//	                 └─ as_pattern_target  (field "alias")
//	                      └─ identifier
func collectWithStatementMCPAliases(pf ParsedFile) map[string]models.MCPServerDef {
	out := make(map[string]models.MCPServerDef)
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "with_statement" {
			return true
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			clause := n.NamedChild(i)
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
				// as_pattern_target normally wraps the bound name as a named
				// child (the identifier); descend to it. The fallback
				// (aliasNode = aliasField) covers grammar shapes where the
				// target node IS the identifier. A non-identifier target
				// (e.g. tuple unpacking `as (a, b)`) yields a non-name key
				// that simply never matches an mcp_servers=[...] item — an
				// acceptable v1 limitation, not a crash.
				aliasNode := aliasField
				if aliasField.NamedChildCount() > 0 {
					aliasNode = aliasField.NamedChild(0)
				}
				fn := callNode.ChildByFieldName("function")
				if fn == nil {
					continue
				}
				name := astutil.NodeText(fn, pf.Source)
				if !IsMCPServerClass(name) {
					continue
				}
				out[astutil.NodeText(aliasNode, pf.Source)] = models.MCPServerDef{
					Class:     name,
					Transport: MCPTransportFromClass(name),
					SDK:       models.SDKOpenAIAgents,
					FilePath:  pf.RelPath,
					Line:      int(callNode.StartPoint().Row) + 1,
				}
			}
		}
		// return true → Walk descends into the with-statement body, so a
		// nested `with` is handled when the callback re-enters on that node.
		return true
	})
	return out
}
