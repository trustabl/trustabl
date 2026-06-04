package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangChain / LangGraph Python agent discovery.
//
// Three constructor-shaped agent forms are recognized (the raw StateGraph graph
// agent is a documented gap — it is emergent across many call sites and does not
// fit the one-call-site = one-agent model):
//
//	create_react_agent(model, tools, prompt=...)   # langgraph.prebuilt (and the
//	                                                 # legacy langchain.agents one)
//	create_agent(model, tools=..., system_prompt=...)  # langchain v1
//	AgentExecutor(agent=..., tools=..., max_iterations=...)
//
// The callee is normalized to a language-agnostic Class (see agentKindMatches in
// internal/rules): "ReactAgent", "CreateAgent", "AgentExecutor". All discovery
// is import-gated to the langchain ecosystem.
var langChainAgentClasses = map[string]string{
	"create_react_agent":                 "ReactAgent",
	"create_agent":                       "CreateAgent",
	"AgentExecutor":                      "AgentExecutor",
	"AgentExecutor.from_agent_and_tools": "AgentExecutor",
}

// DiscoverLangChainAgents walks each ParsedFile and emits one AgentDef per
// recognized LangChain/LangGraph agent constructor call. Only files importing
// the langchain ecosystem are considered.
func DiscoverLangChainAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsLangChain(pf) {
			continue
		}
		out = append(out, discoverLangChainAgentsInFile(pf)...)
	}
	return out
}

func discoverLangChainAgentsInFile(pf ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		class, ok := langChainAgentClasses[callee]
		if !ok {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)

		// create_react_agent(model, tools, ...) and create_agent(model, tools=...)
		// commonly pass the tool list positionally (index 1, after the model).
		// Capture it as a synthetic "tools" kwarg so ResolveEdges and hosted-tool
		// detection see it. AgentExecutor takes tools as a keyword, so it is
		// excluded here. Positional capture beyond index 1 is out of scope.
		if class == "ReactAgent" || class == "CreateAgent" {
			if kwargs == nil || kwargs.Children["tools"] == nil {
				if pos := positionalArgNode(n, 1); pos != nil {
					if kwargs == nil {
						kwargs = &models.KwargTree{Children: map[string]*models.KwargTree{}}
					}
					kwargs.Children["tools"] = exprFromNode(pos, pf.Source)
				}
			}
		}

		a := models.AgentDef{
			SDK:      models.SDKLangChain,
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
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		}
		// Capture the assignment-target identifier (agent = create_react_agent(...))
		// for handoff/edge resolution by variable name.
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

// positionalArgNode returns the index-th positional argument node of a call
// (skipping keyword args, comments, and * / ** splats), or nil if absent.
func positionalArgNode(call *sitter.Node, index int) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	pos := 0
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		switch c.Type() {
		case "keyword_argument", "comment", "dictionary_splat", "list_splat":
			continue
		}
		if pos == index {
			return c
		}
		pos++
	}
	return nil
}
