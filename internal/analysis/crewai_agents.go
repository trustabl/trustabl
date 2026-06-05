package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// CrewAI Python agent discovery.
//
// CrewAI declares agents with a single `Agent(role=, goal=, backstory=, tools=,
// allow_code_execution=, allow_delegation=, ...)` constructor. The class name
// `Agent` collides with the OpenAI Agents SDK and Google ADK's `Agent` alias, so
// all discovery is import-gated to files that import the crewai ecosystem
// (`crewai` / `crewai_tools`). The emitted AgentDef carries SDK=SDKCrewAI and
// Class="Agent"; agentKindMatches("crewai_agent") keys on BOTH, so an OpenAI
// Agent and a CrewAI Agent never cross-match even though both have Class "Agent".
//
// CrewAI TOOLS are the `@tool` decorator from `crewai.tools` — routed through
// discovery.go's kindFromDecorators (import-gated via collectToolImports /
// fileImportsCrewAI), not here. The BaseTool-subclass tool shape is a documented
// v1 gap (the analog of LangChain's `class X(BaseTool)` gap), as is Crew(...)
// orchestration discovery.

// crewAIAgentClasses is the closed set of CrewAI agent constructors. CrewAI has a
// single agent class; the Crew(...) orchestration unit is a documented v1 gap.
var crewAIAgentClasses = map[string]string{
	"Agent": "Agent",
}

// isCrewAIModule reports whether a dotted module path belongs to the CrewAI
// ecosystem: `crewai`, `crewai.*`, `crewai_tools`, `crewai_tools.*`. The
// dot/underscore boundary keeps an unrelated package that merely shares the
// prefix text (e.g. "crewaix") from matching, mirroring isLangChainModule.
func isCrewAIModule(mod string) bool {
	return mod == "crewai" || strings.HasPrefix(mod, "crewai.") ||
		mod == "crewai_tools" || strings.HasPrefix(mod, "crewai_tools.")
}

// fileImportsCrewAI reports whether pf imports the CrewAI ecosystem via a real
// import statement (AST-based, not a source substring — a comment that merely
// mentions crewai must not trip the gate).
func fileImportsCrewAI(pf ParsedFile) bool {
	return fileImportsModule(pf, isCrewAIModule)
}

// DiscoverCrewAIAgents walks each ParsedFile and emits one AgentDef per
// recognized CrewAI Agent(...) constructor call. Only files importing the crewai
// ecosystem are considered (the import gate disambiguates `Agent` from the
// OpenAI and ADK classes of the same name).
func DiscoverCrewAIAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsCrewAI(pf) {
			continue
		}
		out = append(out, discoverCrewAIAgentsInFile(pf)...)
	}
	return out
}

func discoverCrewAIAgentsInFile(pf ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		class, ok := crewAIAgentClasses[callee]
		if !ok {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKCrewAI,
			Class:    class,
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			Kwargs: kwargs,
			Opaque: opaque,
		}
		// CrewAI agents have no name= kwarg; the human-facing label is role=.
		// Fall back to the assignment-target variable when role= is absent or
		// not a string literal.
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		} else if role := kwargStringLiteral(kwargs, "role"); role != "" {
			a.Name = role
		}
		// Capture the assignment-target identifier (researcher = Agent(...)) so
		// tools=[...] and delegation references resolve by variable name.
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
