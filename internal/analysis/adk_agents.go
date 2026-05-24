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
		out = append(out, a)
		return true
	})
	return out
}
