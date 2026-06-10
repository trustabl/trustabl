package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// AutoGen / AG2 Python tool discovery.
//
// AutoGen registers a plain Python function as a model-callable tool in two
// shapes, neither of which is a bare-name decorator (so neither flows through
// discovery.go's kindFromDecorators):
//
//  1. register_function(fn, caller=, executor=, name=, description=) — a call
//     expression. The first positional argument is the function being
//     registered; name= / description= override its metadata. The
//     caller/executor two-agent edge is a documented v1 gap and is not resolved.
//
//  2. Stacked attribute-call decorators:
//
//     @executor.register_for_execution()
//     @assistant.register_for_llm(name="...", description="...")
//     def fn(...): ...
//
//     The callee is an ATTRIBUTE (`<agentvar>.register_for_llm`), not a bare
//     name, so kindFromDecorators (which only resolves unqualified tracked
//     decorator names against imports) does not classify it. This file walks
//     decorated_definition nodes and emits one ToolDef when any decorator's
//     callee attribute suffix is register_for_llm / register_for_execution.
//
// Both shapes emit ToolDef{Kind: KindAutoGenTool, Language: python} with the
// Location pointed at the resolved function body, so the body-scanning
// predicates (has_shell_call / has_code_exec_call / has_dynamic_url_call) find
// the function to inspect. All discovery is import-gated to files that import an
// AutoGen line (fileImportsAutoGen).

// autoGenRegisterDecorators is the set of attribute-call decorator suffixes that
// register a function as an AutoGen tool. register_for_llm advertises the tool to
// the LLM; register_for_execution wires its execution; either (or both, stacked)
// marks the decorated function as a tool.
var autoGenRegisterDecorators = map[string]bool{
	"register_for_llm":       true,
	"register_for_execution": true,
}

// DiscoverAutoGenTools walks each ParsedFile and emits one ToolDef per AutoGen
// tool registration (register_function call or register_for_llm/_execution
// decorator). Only files importing an AutoGen line are considered.
func DiscoverAutoGenTools(files []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if !fileImportsAutoGen(pf) {
			continue
		}
		out = append(out, discoverAutoGenToolsInFile(pf)...)
	}
	return out
}

func discoverAutoGenToolsInFile(pf ParsedFile) []models.ToolDef {
	root := pf.Tree.RootNode()
	funcs := indexTopLevelFunctions(root, pf.Source)

	var out []models.ToolDef
	seen := map[string]bool{} // dedupe by tool name within a file

	// Shape 2 first: register_for_llm / register_for_execution decorators. A
	// function carrying either (or both, stacked) yields exactly one ToolDef.
	for _, dec := range astutil.FindAll(root, "decorated_definition") {
		fn := astutil.FunctionDef(dec)
		if fn == nil {
			continue
		}
		var matched bool
		var description, nameOverride string
		for _, d := range astutil.Decorators(dec) {
			callee := decoratorCallee(d, pf.Source)
			last := callee
			if i := strings.LastIndex(callee, "."); i >= 0 {
				last = callee[i+1:]
			}
			// Require an attribute callee (`<var>.register_for_llm`), not a bare
			// `register_for_llm`, so an unqualified same-named local decorator is
			// not swept up. The dot check is what distinguishes the attribute-call
			// decorator from kindFromDecorators' bare-name path.
			if !strings.Contains(callee, ".") || !autoGenRegisterDecorators[last] {
				continue
			}
			matched = true
			// name= and description= on register_for_llm override the function's
			// own name and docstring (register_for_execution carries no metadata).
			if last == "register_for_llm" {
				if description == "" {
					if v := decoratorStringKwarg(d, "description", pf.Source); v != "" {
						description = v
					}
				}
				if nameOverride == "" {
					if v := decoratorStringKwarg(d, "name", pf.Source); v != "" {
						nameOverride = v
					}
				}
			}
		}
		if !matched {
			continue
		}
		name := nameOverride
		if name == "" {
			name = astutil.FunctionName(fn, pf.Source)
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if description == "" {
			description = astutil.FunctionDocstring(fn, pf.Source)
		}
		out = append(out, buildAutoGenToolFromFunc(fn, pf, name, description))
	}

	// Shape 1: register_function(fn, name=, description=, caller=, executor=).
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		if astutil.NodeText(n.ChildByFieldName("function"), pf.Source) != "register_function" {
			return true
		}
		wrapped := firstPositionalIdent(n, pf.Source)
		if wrapped == "" {
			return true // first arg is a lambda / call / nothing — unresolvable
		}
		fnDef, ok := funcs[wrapped]
		if !ok {
			return true // not a same-file top-level function
		}
		kwargs, _ := extractCallKwargs(n, pf.Source)
		// name= override > wrapped function name.
		name := kwargStringLiteral(kwargs, "name")
		if name == "" {
			name = wrapped
		}
		if seen[name] {
			return true
		}
		seen[name] = true
		// description= override > wrapped function docstring.
		description := kwargStringLiteral(kwargs, "description")
		if description == "" {
			description = astutil.FunctionDocstring(fnDef, pf.Source)
		}
		tool := buildAutoGenToolFromFunc(fnDef, pf, name, description)
		// Capture remaining constructor kwargs (e.g. api_style=) into Config,
		// excluding the metadata/edge kwargs already consumed above.
		if kwargs != nil {
			cfg := map[string]string{}
			for k, child := range kwargs.Children {
				switch k {
				case "name", "description", "caller", "executor":
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
		out = append(out, tool)
		return true
	})

	return out
}

// buildAutoGenToolFromFunc constructs a KindAutoGenTool ToolDef whose Location is
// the resolved function body, so the AST-walking tool predicates (has_shell_call
// / has_code_exec_call / has_dynamic_url_call) inspect the function. Mirrors the
// ADK FunctionTool / LangChain wrapped-function Location convention; the
// shells_out structural fact is stamped for agent-scope reach checks.
func buildAutoGenToolFromFunc(fn *sitter.Node, pf ParsedFile, name, description string) models.ToolDef {
	facts := map[string]string{}
	if pythonBodyShellsOut(fn, pf.Source, CollectShellModuleAliases(pf.Tree.RootNode(), pf.Source)) {
		facts["shells_out"] = "true"
	}
	return models.ToolDef{
		Name:     name,
		Kind:     models.KindAutoGenTool,
		Language: models.LanguagePython,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(fn.StartPoint().Row) + 1,
			EndLine:  int(fn.EndPoint().Row) + 1,
		},
		Description:    description,
		HasTypedParams: astutil.FunctionHasTypedParams(fn, pf.Source),
		ParamNames:     toolParamNames(fn, pf.Source),
		Facts:          facts,
	}
}

// decoratorStringKwarg returns the unquoted value of a string-literal keyword
// argument on a decorator call (e.g. the `description=` in
// `@x.register_for_llm(description="...")`), or "" when absent or non-literal.
func decoratorStringKwarg(d *sitter.Node, key string, src []byte) string {
	if d == nil || d.Type() != "decorator" {
		return ""
	}
	body := d.NamedChild(0)
	if body == nil || body.Type() != "call" {
		return ""
	}
	args := body.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		arg := args.Child(i)
		if arg.Type() != "keyword_argument" {
			continue
		}
		if astutil.NodeText(arg.ChildByFieldName("name"), src) != key {
			continue
		}
		val := arg.ChildByFieldName("value")
		if val == nil || val.Type() != "string" {
			return ""
		}
		return strings.Trim(astutil.NodeText(val, src), `"'`)
	}
	return ""
}
