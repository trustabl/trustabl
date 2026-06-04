package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangChain.js / LangGraph.js TypeScript agent discovery.
//
//	createReactAgent({ llm, tools, prompt })            // @langchain/langgraph/prebuilt (deprecated, dominant)
//	createAgent({ model, tools, systemPrompt, middleware })  // langchain v1
//	new AgentExecutor({ agent, tools, maxIterations })   // @langchain/classic, legacy
//
// Class is normalized to the language-agnostic "ReactAgent" / "CreateAgent" /
// "AgentExecutor" (see agentKindMatches). The raw StateGraph graph agent is a
// documented gap — it is emergent across many call sites and does not fit the
// one-call-site = one-agent model. Provider-package hosted tools (date-stamped
// shell()/bash_*/computer_* from @langchain/openai|anthropic) are also a
// documented gap for v1.

// tsLangChainAgentFactories maps a factory call name to its normalized Class.
var tsLangChainAgentFactories = map[string]string{
	"createReactAgent": "ReactAgent",
	"createAgent":      "CreateAgent",
}

// tsLangChainAgentCtors maps a `new X({...})` constructor to its normalized Class.
var tsLangChainAgentCtors = map[string]string{
	"AgentExecutor": "AgentExecutor",
}

// DiscoverTSLangChainAgents walks each parsed TS file and emits an AgentDef per
// recognized LangChain/LangGraph agent. Import-gated to the langchain ecosystem.
func DiscoverTSLangChainAgents(files []ParsedFile, onFile func(string)) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSLangChainAgentsInFile(pf)...)
	}
	return out
}

func discoverTSLangChainAgentsInFile(pf ParsedFile) []models.AgentDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesMatch(pf.Tree.RootNode(), pf.Source, isTSLangChainModule)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression":
			if class, ok := tsLangChainAgentFactories[astutil.TSCalleeText(n, pf.Source, aliases)]; ok {
				out = append(out, buildTSLangChainAgent(n, pf, class))
			}
		case "new_expression":
			ctor := n.ChildByFieldName("constructor")
			if ctor == nil || ctor.Type() != "identifier" {
				return true
			}
			if class, ok := tsLangChainAgentCtors[aliases[astutil.NodeText(ctor, pf.Source)]]; ok {
				out = append(out, buildTSLangChainAgent(n, pf, class))
			}
		}
		return true
	})
	return out
}

func buildTSLangChainAgent(n *sitter.Node, pf ParsedFile, class string) models.AgentDef {
	a := models.AgentDef{
		SDK:      models.SDKLangChain,
		Class:    class,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(n, pf.Source),
	}
	args := n.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		a.Opaque = true
		return a
	}
	opts := args.NamedChild(0)
	if opts.Type() != "object" {
		a.Opaque = true
		return a
	}
	// A spread (...rest) means the captured kwarg view may be incomplete.
	for i := 0; i < int(opts.NamedChildCount()); i++ {
		if opts.NamedChild(i).Type() == "spread_element" {
			a.Opaque = true
			break
		}
	}
	a.Kwargs = astutil.TSObjectKwargs(opts, pf.Source)
	if nameNode := getObjectProperty(opts, "name", pf.Source); nameNode != nil && nameNode.Type() == "string" {
		a.Name = unquote(astutil.NodeText(nameNode, pf.Source))
	}
	// tools=[...] identifier refs for edge resolution. An inline tool, a ToolNode
	// (new/call expression), or a non-array tools value cannot be enumerated by
	// symbol — mark Opaque so "agent has no tools" rules do not false-fire.
	if tools := getObjectProperty(opts, "tools", pf.Source); tools != nil {
		if tools.Type() != "array" {
			a.Opaque = true
		} else {
			for i := 0; i < int(tools.NamedChildCount()); i++ {
				item := tools.NamedChild(i)
				switch item.Type() {
				case "identifier":
					a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: astutil.NodeText(item, pf.Source)})
				case "new_expression", "call_expression":
					a.Opaque = true
				}
			}
		}
	}
	return a
}
