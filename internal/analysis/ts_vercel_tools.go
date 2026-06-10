package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Vercel AI SDK (TypeScript) tool discovery.
//
// Recognized shapes (import-gated to the bare `ai` module):
//
//	tool({ description, inputSchema, execute })   // v5/v6 — inputSchema
//	tool({ description, parameters, execute })     // v4 — parameters
//	dynamicTool({ description, inputSchema, execute })  // input is always unknown
//
// Unlike LangChain's `tool(fn, {...})` (handler is arg 0, config arg 1) and
// the OpenAI Agents SDK's `tool({...})`, the Vercel factory takes a SINGLE
// options object (arg 0). The disambiguator from the identically-named
// Claude / OpenAI / LangChain `tool()` factories is the `ai`-module import
// gate (isTSVercelModule): a Vercel-importing file routes tool() here and the
// other passes skip it (they gate on their own imports).
//
// A Vercel tool's NAME is derived from the agent's tools-record KEY
// (`tools: { weather: weatherTool }`), not from the tool definition — so the
// emitted ToolDef.Name is empty and the binding identifier is captured as
// VarName. The model sees only the `description` (there is no docstring
// fallback as in Python), so an absent/empty description is the sole authoring
// signal.

// isTSVercelModule reports whether a TS import specifier is the Vercel AI SDK
// core module. Matched exactly (`ai`) — the provider packages (@ai-sdk/*) are
// not import-gated here; provider hosted tools are recognized structurally in
// the agent walk by their `<provider>.tools.<name>` callee shape.
func isTSVercelModule(mod string) bool {
	return mod == "ai"
}

// tsVercelToolFactories maps a recognized factory callee to whether its input
// is dynamic (always `unknown`). `dynamicTool` forces HasTypedParams=false
// regardless of any schema present.
var tsVercelToolFactories = map[string]bool{
	"tool":        false,
	"dynamicTool": true,
}

// DiscoverTSVercelTools walks each parsed TS file and emits a ToolDef per
// recognized Vercel tool factory call. Import-gated to the `ai` module.
func DiscoverTSVercelTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSVercelToolsInFile(pf)...)
	}
	return out
}

func discoverTSVercelToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesMatch(pf.Tree.RootNode(), pf.Source, isTSVercelModule)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		callee := astutil.TSCalleeText(n, pf.Source, aliases)
		dynamic, ok := tsVercelToolFactories[callee]
		if !ok {
			return true
		}
		if td, built := buildTSVercelTool(n, dynamic, pf); built {
			out = append(out, td)
		}
		return true
	})
	return out
}

// buildTSVercelTool builds a ToolDef from a `tool({...})` / `dynamicTool({...})`
// call. dynamic forces HasTypedParams=false (the dynamicTool input is always
// `unknown`).
func buildTSVercelTool(call *sitter.Node, dynamic bool, pf ParsedFile) (models.ToolDef, bool) {
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
		Kind:     models.KindVercelAITool,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(call, pf.Source),
	}
	// description → the only model-visible signal (no docstring fallback in TS).
	if c := kt.Children["description"]; c != nil && c.Value != nil &&
		c.Value.Kind == models.ExprLiteralString {
		td.Description = unquote(c.Value.Text)
	}
	// Params. Two questions matter to the tool rules: does the tool accept
	// model input at all (has_params), and is that input typed (has_typed_params)?
	//
	//   - dynamicTool: input is always `unknown`. It DOES take input but with no
	//     type discipline — record a synthetic "input" param so has_params holds
	//     while HasTypedParams stays false (the VAI-005 "untyped input" signal).
	//   - tool with inputSchema (v5/v6) or parameters (v4):
	//       * a typed Zod object ({ city: z.string() } or z.object({ city: ... }))
	//         -> real param names, HasTypedParams=true.
	//       * an OPEN schema (z.any() / z.unknown() / z.object({}) / {}) -> the
	//         tool takes input but imposes no field types: synthetic "input",
	//         HasTypedParams=false.
	//       * no schema key at all -> no input surface, leave ParamNames empty.
	if dynamic {
		td.ParamNames = []string{"input"}
	} else {
		schema := getObjectProperty(opts, "inputSchema", pf.Source)
		if schema == nil {
			schema = getObjectProperty(opts, "parameters", pf.Source)
		}
		if schema != nil {
			if tsSchemaIsOpen(schema, pf.Source) {
				td.ParamNames = []string{"input"}
			} else if names, typed := tsZodParamNames(schema, pf.Source); typed {
				td.HasTypedParams = true
				td.ParamNames = names
			}
		}
	}
	// execute → body facts (shells_out / code_exec / dynamic_url).
	if exec := getObjectProperty(opts, "execute", pf.Source); exec != nil {
		hc := tsHandlerCapture(exec, pf.Source)
		if len(hc.facts) > 0 {
			td.Facts = hc.facts
		}
		td.HTTPHosts = hc.httpHosts
		td.FSWritePaths = hc.fsWritePaths
	}
	consumed := map[string]bool{
		"description": true, "inputSchema": true, "parameters": true, "execute": true,
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

// tsSchemaIsOpen reports whether a schema value node is an "open" schema that
// imposes no field typing: z.any(), z.unknown(), or an empty z.object({}) /
// empty object literal {}. These mean the model gets no argument-shape
// guidance, so the tool is treated as having untyped params (HasTypedParams
// stays false), matching the Python "no type hints" signal.
func tsSchemaIsOpen(schema *sitter.Node, src []byte) bool {
	switch schema.Type() {
	case "object":
		// `{}` — empty inline object literal.
		return objectHasNoPairs(schema)
	case "call_expression":
		callee := astutil.NodeText(schema.ChildByFieldName("function"), src)
		switch callee {
		case "z.any", "z.unknown":
			return true
		case "z.object":
			// z.object({}) with an empty object argument is open.
			if obj := firstObjectArg(schema); obj != nil {
				return objectHasNoPairs(obj)
			}
			// z.object() with no object arg — treat as open.
			return true
		}
	}
	return false
}

// objectHasNoPairs reports whether an object literal node has no `pair`
// children (i.e. it is `{}` or contains only spreads / shorthand, which carry
// no explicit field types).
func objectHasNoPairs(obj *sitter.Node) bool {
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		if obj.NamedChild(i).Type() == "pair" {
			return false
		}
	}
	return true
}
