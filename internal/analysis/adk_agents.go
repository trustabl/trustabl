package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// adkAgentClasses is the closed set of ADK agent constructors discovery
// recognizes. The `Agent` alias is recognized and normalized to `LlmAgent`
// in the emitted AgentDef.Class.
var adkAgentClasses = map[string]string{
	"LlmAgent":       "LlmAgent",
	"Agent":          "LlmAgent", // TypeAlias = LlmAgent in google.adk.agents
	"SequentialAgent": "SequentialAgent",
	"ParallelAgent":  "ParallelAgent",
	"LoopAgent":      "LoopAgent",
	"LanggraphAgent": "LanggraphAgent",
}

// DiscoverADKAgents walks each ParsedFile and returns AgentDef records for
// every recognized ADK agent constructor call. Only files that import from
// google.adk are considered (the import gate disambiguates `Agent` from
// OpenAI's identically-named class).
func DiscoverADKAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsGoogleADK(pf) {
			continue
		}
		out = append(out, discoverADKAgentsInFile(pf)...)
	}
	return out
}

func fileImportsGoogleADK(pf ParsedFile) bool {
	src := string(pf.Source)
	return strings.Contains(src, "from google.adk") ||
		strings.Contains(src, "import google.adk")
}

func discoverADKAgentsInFile(pf ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		funcName := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		normalized, ok := adkAgentClasses[funcName]
		if !ok {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKGoogleADK,
			Class:    normalized,
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
			Kwargs:   kwargs,
			Opaque:   opaque,
		}
		if kwargs != nil && kwargs.Children["name"] != nil &&
			kwargs.Children["name"].Value != nil &&
			kwargs.Children["name"].Value.Kind == models.ExprLiteralString {
			a.Name = strings.Trim(kwargs.Children["name"].Value.Text, `"'`)
		}
		// Capture the assignment-target identifier (e.g. `greeter = LlmAgent(...)`)
		// so sub_agents=[greeter] references resolve by variable name even when
		// the Python variable differs from the name= literal.
		if p := n.Parent(); p != nil && p.Type() == "assignment" {
			if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
				a.VarName = astutil.NodeText(l, pf.Source)
			}
		}
		out = append(out, a)
		return true
	})
	return out
}

// DiscoverADKTools walks each ParsedFile and emits one ToolDef per
// FunctionTool(symbol) call whose argument resolves to a same-file top-level
// function definition. Cross-module resolution is out of scope.
func DiscoverADKTools(files []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if !fileImportsGoogleADK(pf) {
			continue
		}
		out = append(out, discoverADKToolsInFile(pf)...)
	}
	return out
}

func discoverADKToolsInFile(pf ParsedFile) []models.ToolDef {
	// Index top-level function defs by name for symbol resolution.
	funcs := map[string]*sitter.Node{}
	root := pf.Tree.RootNode()
	for i := 0; i < int(root.NamedChildCount()); i++ {
		c := root.NamedChild(i)
		if c.Type() != "function_definition" {
			continue
		}
		name := astutil.FunctionName(c, pf.Source)
		if name != "" {
			funcs[name] = c
		}
	}

	seen := map[string]bool{}
	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || astutil.NodeText(fn, pf.Source) != "FunctionTool" {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil || args.NamedChildCount() == 0 {
			return true
		}
		first := args.NamedChild(0)
		// Only positional identifier args are resolvable (e.g. FunctionTool(my_fn)).
		// Anything else (FunctionTool(get_callable()), FunctionTool(func=my_fn))
		// is silently skipped — those cannot be analyzed by tool-scope rules.
		if first.Type() != "identifier" {
			return true
		}
		name := astutil.NodeText(first, pf.Source)
		if seen[name] {
			return true
		}
		fnDef, ok := funcs[name]
		if !ok {
			return true
		}
		seen[name] = true
		out = append(out, models.ToolDef{
			Name:           name,
			Kind:           models.KindADKFunctionTool,
			Language:       models.LanguagePython,
			FilePath:       pf.RelPath,
			Line:           int(fnDef.StartPoint().Row) + 1,
			EndLine:        int(fnDef.EndPoint().Row) + 1,
			Description:    astutil.FunctionDocstring(fnDef, pf.Source),
			HasTypedParams: astutil.FunctionHasTypedParams(fnDef),
			ParamNames:     astutil.FunctionParams(fnDef, pf.Source),
		})
		return true
	})
	return out
}
