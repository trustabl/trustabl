package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsADKModules is the set of npm packages whose imports gate @google/adk TS
// discovery. ADK JS is a single package — core/package.json's `exports`
// field declares only the root path, so subpath imports are not valid.
var tsADKModules = []string{"@google/adk"}

// DiscoverTSADKTools walks each parsed TS file and emits a ToolDef per
// `new FunctionTool({...})` constructor call. Import-gated to @google/adk.
//
// Shape note: ADK JS FunctionTool is a CLASS instantiated with an options
// object (`new FunctionTool({name, description, parameters, execute})`),
// not a function-wrapper (Python's `FunctionTool(my_fn)`). Discovery here
// is closer to DiscoverTSOpenAITools (options-object factory) than to
// DiscoverADKTools (bare-function wrapper).
func DiscoverTSADKTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSADKToolsInFile(pf)...)
	}
	return out
}

func discoverTSADKToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsADKModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "new_expression" {
			return true
		}
		// v1 limitation: only bare-identifier constructors are recognized.
		// Namespace-import constructors like `new ns.FunctionTool({...})`
		// (a member_expression) are not handled — extend if that pattern
		// becomes common in real-world @google/adk code.
		ctor := n.ChildByFieldName("constructor")
		if ctor == nil || ctor.Type() != "identifier" {
			return true
		}
		if canon := aliases[astutil.NodeText(ctor, pf.Source)]; canon != "FunctionTool" {
			return true
		}
		if td, ok := extractTSADKTool(n, pf); ok {
			out = append(out, td)
		}
		return true
	})
	return out
}

func extractTSADKTool(call *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
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
		Kind:     models.KindADKFunctionTool,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(call, pf.Source),
	}
	if nameChild := kt.Children["name"]; nameChild != nil && nameChild.Value != nil &&
		nameChild.Value.Kind == models.ExprLiteralString {
		td.Name = unquote(nameChild.Value.Text)
	}
	if descChild := kt.Children["description"]; descChild != nil && descChild.Value != nil &&
		descChild.Value.Kind == models.ExprLiteralString {
		td.Description = unquote(descChild.Value.Text)
	}
	// parameters: inline object literal OR Zod schema constructor — both mean
	// typed params (see tsZodParamNames in ts_openai_tools.go).
	if pNode := getObjectProperty(opts, "parameters", pf.Source); pNode != nil {
		if names, typed := tsZodParamNames(pNode, pf.Source); typed {
			td.HasTypedParams = true
			td.ParamNames = names
		}
	}
	if execNode := getObjectProperty(opts, "execute", pf.Source); execNode != nil {
		hc := tsHandlerCapture(execNode, pf.Source)
		if len(hc.facts) > 0 {
			td.Facts = hc.facts
		}
		td.HTTPHosts = hc.httpHosts
		td.FSWritePaths = hc.fsWritePaths
		td.HTTPMethods = hc.httpMethods
	}
	// Config: flatten every non-consumed kwarg, including nested objects with
	// dot-joined keys (matching the Claude path's flattenKwargs). The leaf-only
	// loop this replaces silently dropped nested config.
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
