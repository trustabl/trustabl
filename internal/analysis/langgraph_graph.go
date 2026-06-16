package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangGraph raw-graph discovery.
//
// A LangGraph graph built imperatively — StateGraph(State) then .add_node /
// .add_edge / .add_conditional_edges then .compile() — is the canonical
// low-level construction surface (the prebuilt create_react_agent / create_agent
// helpers are themselves compiled StateGraphs). Because it is emergent across
// many separate call sites, the single-call create_*_agent discovery in
// langchain_agents.go misses it entirely: a hand-wired graph reports "no
// entities found".
//
// This pass anchors on the StateGraph(...) constructor — the one unambiguous
// "a graph starts here" signal — and emits one AgentDef per graph with Class
// "StateGraph", so the already-scaffolded langchain_state_graph rule token
// matches (see agentKindMatches in internal/rules). Discovery is import-gated to
// the langchain / langgraph ecosystem so an unrelated class named StateGraph is
// not swept up.
//
// The compiled-graph terminus (app = builder.compile(...)) carries the
// security-relevant kwargs (checkpointer, interrupt_before / interrupt_after,
// store). Where the graph is built through a named builder variable, those
// kwargs are linked back onto the agent so rules can read them.

// langGraphBuilderClasses is the set of raw-graph builder constructors. Both
// normalize to Class "StateGraph": MessageGraph is the legacy spelling of the
// same imperative builder and shares the rule surface. The bare base class
// "Graph" is deliberately NOT listed — it is essentially never instantiated
// directly in user code and collides with rdflib / networkx / graphviz / igraph
// `Graph(...)`. Each callee is additionally bound to a langgraph import origin
// (see langChainImports.resolveCallee), so even StateGraph / MessageGraph match
// only when imported from a langgraph module.
var langGraphBuilderClasses = map[string]bool{
	"StateGraph":   true,
	"MessageGraph": true,
}

// DiscoverLangGraphGraphs emits one StateGraph AgentDef per raw LangGraph graph
// builder constructed in a langchain / langgraph-importing file.
func DiscoverLangGraphGraphs(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsLangChain(pf) {
			continue
		}
		out = append(out, discoverLangGraphGraphsInFile(pf)...)
	}
	return out
}

// collectLangGraphToolItems returns the call-shaped items found inside the tool
// lists of ToolNode([...]) and <llm>.bind_tools([...]) calls in a file. A raw
// StateGraph has no tools= kwarg — its tools are wired through these two shapes —
// so these are the dangerous-built-in surface for the graph agent. Only list
// literals are read; a tools list passed by variable is left unresolved (a v1
// limitation). ResolveEdges classifies the dangerous built-ins among the items
// and attaches them to the file's StateGraph agent(s).
func collectLangGraphToolItems(pf ParsedFile) []models.Expr {
	// Index same-file `name = [...]` list assignments so the common
	// `tools = [...]; ToolNode(tools)` / `bind_tools(tools)` variable form
	// resolves to its list literal. Cross-function / cross-module aliases are a
	// v1 limitation.
	listVars := map[string]*sitter.Node{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l != nil && l.Type() == "identifier" && r != nil && r.Type() == "list" {
			listVars[astutil.NodeText(l, pf.Source)] = r
		}
		return true
	})

	var items []models.Expr
	addList := func(list *sitter.Node) {
		for i := 0; i < int(list.NamedChildCount()); i++ {
			el := list.NamedChild(i)
			if el.Type() == "comment" {
				continue
			}
			if e := exprFromNode(el, pf.Source); e != nil && e.Value != nil {
				items = append(items, *e.Value)
			}
		}
	}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		last := callee
		if i := strings.LastIndex(callee, "."); i >= 0 {
			last = callee[i+1:]
		}
		if callee != "ToolNode" && last != "bind_tools" {
			return true
		}
		arg := positionalArgNode(n, 0)
		if arg == nil {
			return true
		}
		switch arg.Type() {
		case "list":
			addList(arg)
		case "identifier":
			if list := listVars[astutil.NodeText(arg, pf.Source)]; list != nil {
				addList(list)
			}
		}
		return true
	})
	return items
}

func discoverLangGraphGraphsInFile(pf ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	// byVar maps a builder variable name -> index into out, so the .compile()
	// pass can attach its kwargs to the right agent.
	byVar := map[string]int{}
	imp := collectLangChainImports(pf)

	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		// Bind the callee to a langgraph import: a bare StateGraph / MessageGraph
		// imported from a langgraph module, or a qualified `lg.StateGraph` whose
		// alias resolves to one. A same-named class from another package is
		// excluded even in a file that also imports langchain.
		if imp.resolveCallee(astutil.NodeText(n.ChildByFieldName("function"), pf.Source), langGraphBuilderClasses) == "" {
			return true
		}
		a := models.AgentDef{
			SDK:      models.SDKLangChain,
			Class:    "StateGraph",
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
		}
		// Capture the assignment-target identifier (builder = StateGraph(...))
		// so the .compile() pass and edge resolution can key on it by name.
		if p := n.Parent(); p != nil && p.Type() == "assignment" {
			if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
				a.VarName = astutil.NodeText(l, pf.Source)
			}
		}
		if a.VarName != "" {
			byVar[a.VarName] = len(out)
		}
		out = append(out, a)
		return true
	})

	if len(out) == 0 {
		return out
	}

	// Second pass: link each `<builder>.compile(...)` call's kwargs onto its
	// agent. compile() is a separate call site from the constructor; a graph
	// built through a named builder variable gets its human-in-the-loop /
	// persistence kwargs attached. The chained form
	// `StateGraph(...).add_node(...).compile()` (no intermediate variable) still
	// yields the agent from the first pass — only its compile kwargs are not
	// linked, an accepted v1 limitation.
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" {
			return true
		}
		if astutil.NodeText(fn.ChildByFieldName("attribute"), pf.Source) != "compile" {
			return true
		}
		recv := fn.ChildByFieldName("object")
		if recv == nil || recv.Type() != "identifier" {
			return true
		}
		idx, ok := byVar[astutil.NodeText(recv, pf.Source)]
		if !ok {
			return true
		}
		kwargs, _ := extractCallKwargs(n, pf.Source)
		if kwargs == nil {
			return true
		}
		if out[idx].Kwargs == nil {
			out[idx].Kwargs = kwargs
			return true
		}
		for k, v := range kwargs.Children {
			out[idx].Kwargs.Children[k] = v
		}
		return true
	})

	return out
}
