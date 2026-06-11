package analysis

import (
	"sort"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsOpenAIAgentsModules is the set of npm packages whose imports gate
// @openai/agents-family TS discovery. The meta package re-exports the
// other two, so a file importing any of them is in scope.
var tsOpenAIAgentsModules = []string{
	"@openai/agents",
	"@openai/agents-core",
	"@openai/agents-openai",
}

// DiscoverTSOpenAITools walks each parsed TS file and emits a ToolDef per
// tool({...}) factory call. Import-gated to the @openai/agents family.
func DiscoverTSOpenAITools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSOpenAIToolsInFile(pf)...)
	}
	return out
}

func discoverTSOpenAIToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsOpenAIAgentsModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		if astutil.TSCalleeText(n, pf.Source, aliases) != "tool" {
			return true
		}
		if td, ok := extractTSOpenAITool(n, pf); ok {
			out = append(out, td)
		}
		return true
	})
	return out
}

func extractTSOpenAITool(call *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return models.ToolDef{}, false
	}
	opts := args.NamedChild(0)
	if opts.Type() != "object" {
		return models.ToolDef{}, false // non-object arg — skip silently
	}
	kt := astutil.TSObjectKwargs(opts, pf.Source)
	td := models.ToolDef{
		Kind:     models.KindOpenAITool,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(call, pf.Source),
	}
	// name and description: top-level string literals
	if nameChild := kt.Children["name"]; nameChild != nil && nameChild.Value != nil &&
		nameChild.Value.Kind == models.ExprLiteralString {
		td.Name = unquote(nameChild.Value.Text)
	}
	if descChild := kt.Children["description"]; descChild != nil && descChild.Value != nil &&
		descChild.Value.Kind == models.ExprLiteralString {
		td.Description = unquote(descChild.Value.Text)
	}
	// parameters: either an inline object literal ({ city: ... }) or a Zod
	// schema constructor (z.object({ city: ... })). Both shapes mean the tool
	// has typed params; tsZodParamNames enumerates the keys for either.
	if pNode := getObjectProperty(opts, "parameters", pf.Source); pNode != nil {
		if names, typed := tsZodParamNames(pNode, pf.Source); typed {
			td.HasTypedParams = true
			td.ParamNames = names
		}
	}
	// execute: walk for body facts + Stage 2 typed captures
	if execNode := getObjectProperty(opts, "execute", pf.Source); execNode != nil {
		hc := tsHandlerCapture(execNode, pf.Source)
		if len(hc.facts) > 0 {
			td.Facts = hc.facts
		}
		td.HTTPHosts = hc.httpHosts
		td.FSWritePaths = hc.fsWritePaths
		td.HTTPMethods = hc.httpMethods
		td.HTTPCalls = hc.httpCalls
	}
	// Config: flatten every non-consumed kwarg, including nested objects with
	// dot-joined keys (matching the Claude path's flattenKwargs). The leaf-only
	// loop this replaces silently dropped nested config like `annotations.*`.
	consumed := map[string]bool{
		"name": true, "description": true, "parameters": true, "execute": true,
	}
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

// tsZodParamNames extracts a TS tool's parameter names from the value node of
// its `parameters:` property, handling the two real-world shapes:
//
//	parameters: { city: z.string() }      // inline object literal
//	parameters: z.object({ city: ... })   // Zod schema constructor (a call)
//
// The bool reports whether the tool has typed params at all. A schema
// constructor (any call expression) always implies typed params — even a
// chained `z.object({...}).strict()` or a form whose keys cannot be read —
// because the alternative (treating it as untyped) mass-false-positives the
// "tool has untyped params" rules on idiomatic Zod tools. Names are sorted so
// the serialized ParamNames slice is deterministic.
func tsZodParamNames(params *sitter.Node, src []byte) (names []string, typed bool) {
	if params == nil {
		return nil, false
	}
	switch params.Type() {
	case "object":
		names = objectKeyNames(params, src)
		return names, len(names) > 0
	case "call_expression":
		// Pull keys from the object literal passed to the innermost call
		// (z.object({...})); a schema constructor means typed even if empty.
		if obj := firstObjectArg(params); obj != nil {
			names = objectKeyNames(obj, src)
		}
		return names, true
	}
	return nil, false
}

// objectKeyNames returns the sorted property-key names of a TS object literal.
func objectKeyNames(obj *sitter.Node, src []byte) []string {
	kt := astutil.TSObjectKwargs(obj, src)
	if len(kt.Children) == 0 {
		return nil
	}
	names := make([]string, 0, len(kt.Children))
	for k := range kt.Children {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// firstObjectArg returns the first object-literal argument of a call
// expression, unwrapping chained method calls so that
// `z.object({...}).strict().describe(...)` still yields the inner z.object's
// object argument. Returns nil if no object-literal argument is found.
func firstObjectArg(call *sitter.Node) *sitter.Node {
	for call != nil && call.Type() == "call_expression" {
		if args := call.ChildByFieldName("arguments"); args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				if a := args.NamedChild(i); a.Type() == "object" {
					return a
				}
			}
		}
		// Unwrap one chained call: `inner(...).method(...)` — descend to inner.
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			break
		}
		call = fn.ChildByFieldName("object")
	}
	return nil
}

// unquote strips one leading/trailing quote pair from a JS string literal
// text (which always comes from tree-sitter with surrounding quotes intact).
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	return s[1 : len(s)-1]
}
