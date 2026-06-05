package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Pydantic AI Python tool discovery (non-decorator factory shape).
//
// The `@<agentvar>.tool` / `@<agentvar>.tool_plain` attribute decorators are
// handled in discovery.go's kindFromDecorators (import-gated, with the Claude
// SDK winning the `@agent.tool` collision). This file handles the NON-decorator
// `Tool(...)` factory, a plain call expression:
//
//	Tool(fn, takes_ctx=False, requires_approval=False, name="...", description="...")
//
// The first positional argument is the function being registered; explicit
// name= / description= kwargs override its metadata. Discovery is gated on
// fileImportsPydanticAI AND NOT fileImportsLangChain: LangChain ships an
// identically-named `Tool(...)` factory (langChainToolFactories), and a file
// importing both SDKs must not double-emit the same call — LangChain's gate
// wins there, exactly as the Claude SDK wins the @agent.tool decorator
// collision. The bare-function `tools=[fn]` ToolDef-synthesis shape is a
// documented v1 gap (the agent edge still works via the tools= kwarg).

// DiscoverPydanticAITools walks each ParsedFile and emits one ToolDef per
// recognized Pydantic AI Tool(...) factory call. Only files importing
// pydantic_ai (and not the LangChain ecosystem, which owns the colliding
// Tool(...) name) are considered.
func DiscoverPydanticAITools(files []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if !fileImportsPydanticAI(pf) || fileImportsLangChain(pf) {
			continue
		}
		out = append(out, discoverPydanticAIToolsInFile(pf)...)
	}
	return out
}

func discoverPydanticAIToolsInFile(pf ParsedFile) []models.ToolDef {
	root := pf.Tree.RootNode()
	funcs := indexTopLevelFunctions(root, pf.Source)

	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		if astutil.NodeText(n.ChildByFieldName("function"), pf.Source) != "Tool" {
			return true
		}
		if tool, ok := buildPydanticAITool(n, pf, funcs); ok {
			out = append(out, tool)
		}
		return true
	})
	return out
}

// buildPydanticAITool extracts a ToolDef from a Pydantic AI `Tool(fn, ...)`
// factory call. It resolves the first positional ident to a same-file function
// (mirroring buildLangChainTool's from_function resolution) to recover the
// docstring, parameter typing, and shell-out fact; explicit name= / description=
// kwargs override the wrapped function's values. The Location points at the
// resolved function body so the AST-walking predicates (has_shell_call /
// has_code_exec_call / has_dynamic_url_call) find the body to scan. Returns
// ok=false when no tool name can be determined.
func buildPydanticAITool(n *sitter.Node, pf ParsedFile, funcs map[string]*sitter.Node) (models.ToolDef, bool) {
	kwargs, _ := extractCallKwargs(n, pf.Source)

	// Resolve the wrapped function symbol: the first positional argument.
	wrappedName := firstPositionalIdent(n, pf.Source)
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

	var (
		hasTyped bool
		params   []string
	)
	facts := map[string]string{}
	if fnDef != nil {
		hasTyped = astutil.FunctionHasTypedParams(fnDef, pf.Source)
		params = toolParamNames(fnDef, pf.Source)
		if pythonBodyShellsOut(fnDef, pf.Source) {
			facts["shells_out"] = "true"
		}
	}

	// Point the location at the wrapped function body when resolved; otherwise at
	// the factory call site. The body-scanning predicates only see the function
	// when the location is on it (i.e. the wrapped function was resolved).
	line, endLine := int(n.StartPoint().Row)+1, int(n.EndPoint().Row)+1
	if fnDef != nil {
		line, endLine = int(fnDef.StartPoint().Row)+1, int(fnDef.EndPoint().Row)+1
	}
	tool := models.ToolDef{
		Name:     name,
		Kind:     models.KindPydanticAITool,
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
	// Capture remaining constructor kwargs (e.g. takes_ctx=, requires_approval=)
	// into Config so any tool_decorator_kwarg_value rule can read them.
	if kwargs != nil {
		cfg := map[string]string{}
		for k, child := range kwargs.Children {
			switch k {
			case "name", "description":
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
	// Capture the assignment-target identifier (my_tool = Tool(fn)) so agent
	// tools=[my_tool] references resolve by variable name.
	if p := n.Parent(); p != nil && p.Type() == "assignment" {
		if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
			tool.VarName = astutil.NodeText(l, pf.Source)
		}
	}
	return tool, true
}
