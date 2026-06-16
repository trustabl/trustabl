package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangChain / LangGraph Python agent discovery.
//
// Three constructor-shaped agent forms are recognized here. The raw StateGraph
// graph agent (emergent across many call sites) does not fit this
// one-call-site = one-agent model and is discovered separately in
// langgraph_graph.go:
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
	"create_react_agent": "ReactAgent",
	"create_agent":       "CreateAgent",
	"AgentExecutor":      "AgentExecutor",
}

// langChainAgentClassNames is the bare-name set used for import-binding
// resolution (mirrors langChainAgentClasses' keys).
var langChainAgentClassNames = map[string]bool{
	"create_react_agent": true, "create_agent": true, "AgentExecutor": true,
}

// resolveLangChainAgentClass returns the normalized AgentDef.Class when callee
// names a LangChain agent constructor bound to a langchain / langgraph import: a
// bare or module-qualified create_react_agent / create_agent / AgentExecutor, or
// the AgentExecutor.from_agent_and_tools classmethod on a langchain-imported
// AgentExecutor. Returns "" otherwise, so a locally-defined or unrelated
// same-named callable (a user `def create_agent`, `module.create_agent` from a
// non-langchain package) is not discovered.
func resolveLangChainAgentClass(callee string, imp langChainImports) string {
	if dot := strings.LastIndex(callee, "."); dot >= 0 {
		object, attr := callee[:dot], callee[dot+1:]
		if attr == "from_agent_and_tools" && imp.names[object] {
			return "AgentExecutor"
		}
	}
	if name := imp.resolveCallee(callee, langChainAgentClassNames); name != "" {
		return langChainAgentClasses[name]
	}
	return ""
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
	imp := collectLangChainImports(pf)
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		class := resolveLangChainAgentClass(astutil.NodeText(n.ChildByFieldName("function"), pf.Source), imp)
		if class == "" {
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
				if pos := positionalArgNode(n, 1); pos != nil && positionalLooksLikeTools(pos) {
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

// positionalLooksLikeTools reports whether a positional arg node could be a
// tools collection (a list, or an identifier/call/attribute/subscript that may
// resolve to one) rather than a scalar literal like a prompt string. Guards the
// synthetic tools-kwarg capture so create_react_agent(model, "be helpful") does
// not record the prompt string as the agent's tools list.
func positionalLooksLikeTools(n *sitter.Node) bool {
	switch n.Type() {
	case "string", "concatenated_string", "integer", "float", "true", "false", "none":
		return false
	}
	return true
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
