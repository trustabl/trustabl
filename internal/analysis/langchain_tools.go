package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangChain / LangGraph Python tool discovery.
//
// The @tool decorator is handled in discovery.go's kindFromDecorators: it shares
// the decorated_definition shape with the Claude/OpenAI/MCP decorators and is
// routed there, import-gated to disambiguate from the Claude SDK's own @tool.
// This file handles the NON-decorator LangChain tool builders, which are plain
// call expressions:
//
//	StructuredTool.from_function(fn, name=..., description=..., args_schema=...)
//	StructuredTool(name=..., description=..., func=fn, args_schema=...)
//	Tool.from_function(fn, name=..., description=...)
//	Tool(name=..., description=..., func=fn)
//
// All are import-gated to files that import the langchain ecosystem, so a
// user-defined Tool(...) in a non-langchain file is not swept up. Class-based
// tools (`class X(BaseTool)`) are a documented gap — the least common shape.

// langChainToolFactories is the closed set of call callees that build a
// LangChain tool, keyed by the resolved callee path.
var langChainToolFactories = map[string]bool{
	"StructuredTool":               true,
	"StructuredTool.from_function": true,
	"Tool":                         true,
	"Tool.from_function":           true,
}

// isLangChainModule reports whether a dotted module path belongs to the
// LangChain / LangGraph ecosystem: langchain, langchain_core, langchain_community,
// langchain_experimental, langchain_classic, the langchain-* provider packages,
// langgraph, and langgraph_* (supervisor / swarm). The dot/underscore boundary
// keeps an unrelated package that merely shares the prefix text (e.g.
// "langchainx") from matching, mirroring isGoogleADKModule's discipline.
func isLangChainModule(mod string) bool {
	return mod == "langchain" || strings.HasPrefix(mod, "langchain.") ||
		strings.HasPrefix(mod, "langchain_") ||
		mod == "langgraph" || strings.HasPrefix(mod, "langgraph.") ||
		strings.HasPrefix(mod, "langgraph_")
}

// fileImportsLangChain reports whether pf imports the LangChain / LangGraph
// ecosystem via a real import statement (AST-based, not a source substring — a
// comment that merely mentions langchain must not trip the gate).
func fileImportsLangChain(pf ParsedFile) bool {
	return fileImportsModule(pf, isLangChainModule)
}

// langChainImports records, per file, how langchain / langgraph symbols are
// bound, so a bare or module-qualified constructor callee can be tied to its
// import origin. This prevents a same-named class from an unrelated package
// (networkx.Graph, a locally-defined StateGraph) from matching by bare name in
// any file that merely also imports the ecosystem.
type langChainImports struct {
	names   map[string]bool // local names imported FROM a langchain/langgraph module
	aliases map[string]bool // module aliases bound to a langchain/langgraph module
}

// collectLangChainImports walks pf's import statements and records langchain /
// langgraph symbol bindings (see langChainImports). The module_name node of a
// `from X import Y` is skipped by byte offset so the module is not mistaken for
// an imported name.
func collectLangChainImports(pf ParsedFile) langChainImports {
	res := langChainImports{names: map[string]bool{}, aliases: map[string]bool{}}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			mod := n.ChildByFieldName("module_name")
			if mod == nil || !isLangChainModule(astutil.NodeText(mod, pf.Source)) {
				return true
			}
			modStart := mod.StartByte()
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				if c.StartByte() == modStart {
					continue // the module_name itself, not an imported name
				}
				switch c.Type() {
				case "dotted_name", "identifier":
					res.names[astutil.NodeText(c, pf.Source)] = true
				case "aliased_import":
					if alias := astutil.NodeText(c.ChildByFieldName("alias"), pf.Source); alias != "" {
						res.names[alias] = true
					}
				}
			}
		case "import_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					if m := astutil.NodeText(c, pf.Source); isLangChainModule(m) {
						res.aliases[m] = true
					}
				case "aliased_import":
					m := astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
					alias := astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)
					if isLangChainModule(m) && alias != "" {
						res.aliases[alias] = true
					}
				}
			}
		}
		return true
	})
	return res
}

// resolveCallee returns the matched class name when callee (a call's function
// text) names a member of classNames AND is bound to a langchain / langgraph
// import: a bare name imported from a langchain module, or a module-qualified
// `<object>.<Class>` whose `<object>` resolves to a langchain module alias (or
// is itself a langchain dotted module). Returns "" otherwise, so a same-named
// class from an unrelated package is never matched.
func (imp langChainImports) resolveCallee(callee string, classNames map[string]bool) string {
	if dot := strings.LastIndex(callee, "."); dot >= 0 {
		object, attr := callee[:dot], callee[dot+1:]
		if classNames[attr] && (imp.aliases[object] || isLangChainModule(object)) {
			return attr
		}
		return ""
	}
	if classNames[callee] && imp.names[callee] {
		return callee
	}
	return ""
}

// fileImportsClaudeSDK reports whether pf imports the Claude Agent SDK. Used to
// resolve the @tool decorator collision: an @tool in a file that imports the
// Claude SDK stays Claude even if the file also imports langchain.
func fileImportsClaudeSDK(pf ParsedFile) bool {
	return fileImportsModule(pf, func(mod string) bool {
		return mod == "claude_agent_sdk" || strings.HasPrefix(mod, "claude_agent_sdk.")
	})
}

// fileImportsModule walks pf's import statements and reports whether any imported
// module satisfies match. Parameterized sibling of fileImportsGoogleADK.
func fileImportsModule(pf ParsedFile, match func(string) bool) bool {
	found := false
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if found {
			return false
		}
		switch n.Type() {
		case "import_from_statement":
			if match(astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)) {
				found = true
			}
		case "import_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					if match(astutil.NodeText(c, pf.Source)) {
						found = true
					}
				case "aliased_import":
					if match(astutil.NodeText(c.ChildByFieldName("name"), pf.Source)) {
						found = true
					}
				}
			}
		}
		return true
	})
	return found
}

// DiscoverLangChainTools walks each ParsedFile and emits one ToolDef per
// recognized non-decorator LangChain tool builder. Only files importing the
// langchain ecosystem are considered.
func DiscoverLangChainTools(files []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if !fileImportsLangChain(pf) {
			continue
		}
		out = append(out, discoverLangChainToolsInFile(pf)...)
	}
	return out
}

func discoverLangChainToolsInFile(pf ParsedFile) []models.ToolDef {
	root := pf.Tree.RootNode()
	funcs := indexTopLevelFunctions(root, pf.Source)

	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		if !langChainToolFactories[callee] {
			return true
		}
		if tool, ok := buildLangChainTool(n, callee, pf, funcs); ok {
			out = append(out, tool)
		}
		return true
	})
	return out
}

// buildLangChainTool extracts a ToolDef from a StructuredTool/Tool factory call.
// It resolves a same-file wrapped function — the first positional arg for the
// from_function form, or the func= kwarg for the constructor form — to recover
// the docstring, parameter typing, and shell-out fact; explicit name= /
// description= / args_schema= kwargs override the wrapped function's values.
// Returns ok=false when no tool name can be determined.
func buildLangChainTool(n *sitter.Node, callee string, pf ParsedFile, funcs map[string]*sitter.Node) (models.ToolDef, bool) {
	kwargs, _ := extractCallKwargs(n, pf.Source)

	// Resolve the wrapped function symbol.
	var wrappedName string
	if strings.HasSuffix(callee, ".from_function") {
		wrappedName = firstPositionalIdent(n, pf.Source)
	}
	if wrappedName == "" && kwargs != nil {
		if fk := kwargs.Children["func"]; fk != nil && fk.Value != nil && fk.Value.Kind == models.ExprNameRef {
			wrappedName = fk.Value.Text
		}
	}
	var fnDef *sitter.Node
	if wrappedName != "" {
		fnDef = funcs[wrappedName]
	}

	// Name: explicit name= kwarg > wrapped function name.
	name := kwargStringLiteral(kwargs, "name")
	if name == "" {
		name = wrappedName
	}
	if name == "" {
		return models.ToolDef{}, false
	}

	// Description: explicit description= kwarg > wrapped function docstring.
	description := kwargStringLiteral(kwargs, "description")
	if description == "" && fnDef != nil {
		description = astutil.FunctionDocstring(fnDef, pf.Source)
	}

	// Typed params: an args_schema (a Pydantic model) is a typed contract; else
	// fall back to the wrapped function's signature typing.
	hasTyped := kwargs != nil && kwargs.Children["args_schema"] != nil
	if !hasTyped && fnDef != nil {
		hasTyped = astutil.FunctionHasTypedParams(fnDef, pf.Source)
	}

	var params []string
	facts := map[string]string{}
	if fnDef != nil {
		params = toolParamNames(fnDef, pf.Source)
		if pythonBodyShellsOut(fnDef, pf.Source) {
			facts["shells_out"] = "true"
		}
	}

	// Point the location at the wrapped function body when resolved (mirrors ADK
	// FunctionTool) so the AST-walking predicates (has_shell_call /
	// has_code_exec_call / has_dynamic_url_call) find the body to scan.
	// FindFunctionNode matches on (line, name), so this is effective only when the
	// tool Name is the wrapped function's name — i.e. no name= override. With a
	// name= override the tool keeps full field-based coverage (description, typed
	// params, return_direct) but not the AST-walked body checks.
	line, endLine := int(n.StartPoint().Row)+1, int(n.EndPoint().Row)+1
	if fnDef != nil {
		line, endLine = int(fnDef.StartPoint().Row)+1, int(fnDef.EndPoint().Row)+1
	}
	tool := models.ToolDef{
		Name:     name,
		Kind:     models.KindLangChainTool,
		Language: models.LanguagePython,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     line,
			EndLine:  endLine,
		},
		Description:    description,
		HasTypedParams: hasTyped,
		ParamNames:     params,
		Facts:          facts,
	}
	// Capture remaining constructor kwargs (e.g. return_direct=True) into Config so
	// tool_decorator_kwarg_value rules can read them. The @tool decorator form
	// gets Config via extractDecoratorKwargs in discovery.go; this is the
	// constructor-form equivalent.
	if kwargs != nil {
		cfg := map[string]string{}
		for k, child := range kwargs.Children {
			switch k {
			case "name", "description", "func", "args_schema":
				continue
			}
			if child.Value != nil {
				cfg[k] = child.Value.Text
			}
		}
		if len(cfg) > 0 {
			tool.Config = cfg
		}
	}
	// Capture the assignment-target identifier (my_tool = StructuredTool(...)) so
	// agent tools=[my_tool] references resolve by variable name.
	if p := n.Parent(); p != nil && p.Type() == "assignment" {
		if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
			tool.VarName = astutil.NodeText(l, pf.Source)
		}
	}
	return tool, true
}

// indexTopLevelFunctions maps top-level function names to their definition nodes.
func indexTopLevelFunctions(root *sitter.Node, src []byte) map[string]*sitter.Node {
	funcs := map[string]*sitter.Node{}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		c := root.NamedChild(i)
		if c.Type() != "function_definition" {
			continue
		}
		if name := astutil.FunctionName(c, src); name != "" {
			funcs[name] = c
		}
	}
	return funcs
}

// firstPositionalIdent returns the text of the first positional identifier arg of
// a call, or "" if the first positional arg is not a bare identifier (e.g. a
// lambda or a nested call, which cannot be resolved to a same-file definition).
func firstPositionalIdent(n *sitter.Node, src []byte) string {
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" || c.Type() == "comment" {
			continue
		}
		if c.Type() == "identifier" {
			return astutil.NodeText(c, src)
		}
		return ""
	}
	return ""
}

// kwargStringLiteral returns the unquoted value of a string-literal kwarg, or "".
func kwargStringLiteral(kwargs *models.KwargTree, key string) string {
	if kwargs == nil {
		return ""
	}
	c := kwargs.Children[key]
	if c == nil || c.Value == nil || c.Value.Kind != models.ExprLiteralString {
		return ""
	}
	return strings.Trim(c.Value.Text, `"'`)
}
