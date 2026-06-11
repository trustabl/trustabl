package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangChain.js / LangGraph.js TypeScript tool discovery.
//
// Recognized shapes (import-gated to the langchain ecosystem):
//
//	tool(fn, { name, description, schema })            // @langchain/core/tools or langchain
//	new DynamicStructuredTool({ name, description, schema, func })
//	new DynamicTool({ name, description, func })        // no schema (string in / out)
//
// Class-based tools (`class X extends StructuredTool`) are a documented gap.
//
// Collision note: the bare tool(...) factory is shared with the Claude SDK and
// the OpenAI Agents SDK. The import gate (isTSLangChainModule) is what keeps the
// three apart — a langchain-importing file routes tool() here, while the Claude
// and OpenAI passes gate on their own imports and skip langchain files.

// isTSLangChainModule reports whether a TS import specifier belongs to the
// LangChain / LangGraph ecosystem. Prefix-based because real code imports from
// many subpaths (@langchain/core/tools, @langchain/langgraph/prebuilt, …).
func isTSLangChainModule(mod string) bool {
	return mod == "langchain" || strings.HasPrefix(mod, "langchain/") ||
		mod == "langgraph" || strings.HasPrefix(mod, "langgraph/") ||
		strings.HasPrefix(mod, "@langchain/")
}

// tsLangChainToolCtors is the set of `new X({...})` tool constructors.
var tsLangChainToolCtors = map[string]bool{
	"DynamicStructuredTool": true,
	"DynamicTool":           true,
}

// DiscoverTSLangChainTools walks each parsed TS file and emits a ToolDef per
// recognized LangChain tool builder. Import-gated to the langchain ecosystem.
func DiscoverTSLangChainTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSLangChainToolsInFile(pf)...)
	}
	return out
}

func discoverTSLangChainToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesMatch(pf.Tree.RootNode(), pf.Source, isTSLangChainModule)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression":
			if astutil.TSCalleeText(n, pf.Source, aliases) != "tool" {
				return true
			}
			// tool(fn, { ...config }) — config is arg 1, the handler fn is arg 0.
			args := n.ChildByFieldName("arguments")
			if args == nil || args.NamedChildCount() < 2 {
				return true
			}
			if td, ok := buildTSLangChainTool(n, args.NamedChild(1), args.NamedChild(0), pf); ok {
				out = append(out, td)
			}
		case "new_expression":
			ctor := n.ChildByFieldName("constructor")
			if ctor == nil || ctor.Type() != "identifier" {
				return true
			}
			if !tsLangChainToolCtors[aliases[astutil.NodeText(ctor, pf.Source)]] {
				return true
			}
			// new DynamicStructuredTool({ ...config, func }) — config is arg 0.
			args := n.ChildByFieldName("arguments")
			if args == nil || args.NamedChildCount() == 0 {
				return true
			}
			if td, ok := buildTSLangChainTool(n, args.NamedChild(0), nil, pf); ok {
				out = append(out, td)
			}
		}
		return true
	})
	return out
}

// buildTSLangChainTool builds a ToolDef from a config object literal. funcNode,
// when non-nil, is an external handler node (the tool() factory's arg 0) walked
// for body facts; otherwise the config's own `func` property is the handler.
func buildTSLangChainTool(node, optsObj, funcNode *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
	if optsObj == nil || optsObj.Type() != "object" {
		return models.ToolDef{}, false
	}
	kt := astutil.TSObjectKwargs(optsObj, pf.Source)
	td := models.ToolDef{
		Kind:     models.KindLangChainTool,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(node.StartPoint().Row) + 1,
			EndLine:  int(node.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(node, pf.Source),
	}
	if c := kt.Children["name"]; c != nil && c.Value != nil && c.Value.Kind == models.ExprLiteralString {
		td.Name = unquote(c.Value.Text)
	}
	if c := kt.Children["description"]; c != nil && c.Value != nil && c.Value.Kind == models.ExprLiteralString {
		td.Description = unquote(c.Value.Text)
	}
	// schema → typed params (Zod object or inline literal). LangChain uses
	// `schema`, not OpenAI/ADK's `parameters`.
	if s := getObjectProperty(optsObj, "schema", pf.Source); s != nil {
		if names, typed := tsZodParamNames(s, pf.Source); typed {
			td.HasTypedParams = true
			td.ParamNames = names
		}
	}
	// Body facts: prefer the external handler (tool() arg 0); else the func: prop.
	handler := funcNode
	if handler == nil {
		handler = getObjectProperty(optsObj, "func", pf.Source)
	}
	if handler != nil {
		hc := tsHandlerCapture(handler, pf.Source)
		if len(hc.facts) > 0 {
			td.Facts = hc.facts
		}
		td.HTTPHosts = hc.httpHosts
		td.FSWritePaths = hc.fsWritePaths
		td.HTTPMethods = hc.httpMethods
	}
	consumed := map[string]bool{"name": true, "description": true, "schema": true, "func": true}
	cfg := map[string]string{}
	for k, child := range kt.Children {
		if consumed[k] {
			continue
		}
		if child.Value != nil {
			cfg[k] = child.Value.Text
		} else {
			flattenKwargs(k, child, cfg)
		}
	}
	if len(cfg) > 0 {
		td.Config = cfg
	}
	return td, true
}
