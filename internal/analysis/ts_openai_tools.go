package analysis

import (
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
	// parameters: top-level keys become ParamNames
	if pChild := kt.Children["parameters"]; pChild != nil && len(pChild.Children) > 0 {
		td.HasTypedParams = true
		for k := range pChild.Children {
			td.ParamNames = append(td.ParamNames, k)
		}
	}
	// execute: walk for body facts
	if execNode := getObjectProperty(opts, "execute", pf.Source); execNode != nil {
		facts := tsHandlerFacts(execNode, pf.Source)
		if len(facts) > 0 {
			td.Facts = facts
		}
	}
	// Config: flatten the leaf kwargs that aren't already consumed above
	consumed := map[string]bool{
		"name": true, "description": true, "parameters": true, "execute": true,
	}
	for k, child := range kt.Children {
		if consumed[k] || child.Value == nil {
			continue
		}
		if td.Config == nil {
			td.Config = map[string]string{}
		}
		td.Config[k] = child.Value.Text
	}
	return td, true
}

// unquote strips one leading/trailing quote pair from a JS string literal
// text (which always comes from tree-sitter with surrounding quotes intact).
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	return s[1 : len(s)-1]
}
