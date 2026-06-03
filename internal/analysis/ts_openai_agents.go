package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverTSOpenAIAgents walks each parsed TS file and emits an AgentDef per
// `new Agent({...})` expression OR `Agent.create({...})` static factory call,
// import-gated to the @openai/agents family.
//
// HostedToolRefs are pre-resolved at discovery time (the alias map is local
// to this pass; threading it into ResolveEdges would be a bigger refactor).
// Other refs (ToolRefs, HandoffRefs, InputGuards, OutputGuards, MCPServerRefs)
// are left as identifier names for ResolveEdges to wire by binding name.
func DiscoverTSOpenAIAgents(files []ParsedFile, onFile func(string)) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSOpenAIAgentsInFile(pf)...)
	}
	return out
}

func discoverTSOpenAIAgentsInFile(pf ParsedFile) []models.AgentDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsOpenAIAgentsModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "new_expression":
			if a, ok := extractTSOpenAINewAgent(n, pf, aliases); ok {
				out = append(out, a)
			}
		case "call_expression":
			if a, ok := extractTSOpenAIAgentCreate(n, pf, aliases); ok {
				out = append(out, a)
			}
		}
		return true
	})
	return out
}

// extractTSOpenAINewAgent matches `new Agent({...})` where Agent resolves
// via the alias map.
func extractTSOpenAINewAgent(n *sitter.Node, pf ParsedFile, aliases map[string]string) (models.AgentDef, bool) {
	ctor := n.ChildByFieldName("constructor")
	if ctor == nil || ctor.Type() != "identifier" {
		return models.AgentDef{}, false
	}
	if canon := aliases[astutil.NodeText(ctor, pf.Source)]; canon != "Agent" {
		return models.AgentDef{}, false
	}
	return buildTSOpenAIAgent(n, pf, aliases), true
}

// extractTSOpenAIAgentCreate matches `Agent.create({...})` where Agent
// resolves to the canonical name via the alias map.
func extractTSOpenAIAgentCreate(n *sitter.Node, pf ParsedFile, aliases map[string]string) (models.AgentDef, bool) {
	fn := n.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return models.AgentDef{}, false
	}
	obj := fn.ChildByFieldName("object")
	prop := fn.ChildByFieldName("property")
	if obj == nil || prop == nil || obj.Type() != "identifier" {
		return models.AgentDef{}, false
	}
	if aliases[astutil.NodeText(obj, pf.Source)] != "Agent" {
		return models.AgentDef{}, false
	}
	if astutil.NodeText(prop, pf.Source) != "create" {
		return models.AgentDef{}, false
	}
	return buildTSOpenAIAgent(n, pf, aliases), true
}

// buildTSOpenAIAgent is the shared body for `new Agent({...})` and
// `Agent.create({...})`. The caller has already validated that the call
// targets the canonical Agent symbol.
func buildTSOpenAIAgent(n *sitter.Node, pf ParsedFile, aliases map[string]string) models.AgentDef {
	a := models.AgentDef{
		SDK:      models.SDKOpenAIAgents,
		Class:    "Agent",
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
	// Detect spread (...rest) — TSObjectKwargs silently skips spread, so we
	// flag opaque ourselves so rules know the kwarg view may be incomplete.
	for i := 0; i < int(opts.NamedChildCount()); i++ {
		if opts.NamedChild(i).Type() == "spread_element" {
			a.Opaque = true
			break
		}
	}
	a.Kwargs = astutil.TSObjectKwargs(opts, pf.Source)
	if nameNode := getObjectProperty(opts, "name", pf.Source); nameNode != nil &&
		nameNode.Type() == "string" {
		a.Name = unquote(astutil.NodeText(nameNode, pf.Source))
	}
	populateTSOpenAIToolRefs(&a, opts, pf, aliases)
	populateTSOpenAIIdentifierList(opts, "handoffs", pf.Source, func(name string) {
		a.HandoffRefs = append(a.HandoffRefs, models.AgentRef{Name: name})
	})
	populateTSOpenAIIdentifierList(opts, "inputGuardrails", pf.Source, func(name string) {
		a.InputGuards = append(a.InputGuards, models.GuardrailRef{Name: name})
	})
	populateTSOpenAIIdentifierList(opts, "outputGuardrails", pf.Source, func(name string) {
		a.OutputGuards = append(a.OutputGuards, models.GuardrailRef{Name: name})
	})
	populateTSOpenAIIdentifierList(opts, "mcpServers", pf.Source, func(name string) {
		// Class holds the identifier text at discovery; ResolveEdges replaces
		// it with the canonical MCP server class on successful resolution.
		a.MCPServerRefs = append(a.MCPServerRefs, models.MCPServerRef{
			Class:    name,
			DefIndex: -1,
		})
	})
	return a
}

// populateTSOpenAIToolRefs walks options.tools (must be an array) and for
// each item:
//   - call_expression whose alias-resolved callee is a hosted-tool factory →
//     emit a HostedToolRef on the agent
//   - identifier → emit a ToolRef{Name: identifier text} for ResolveEdges
//
// Items of any other kind (spread, computed expr) are skipped.
func populateTSOpenAIToolRefs(a *models.AgentDef, opts *sitter.Node, pf ParsedFile, aliases map[string]string) {
	tools := getObjectProperty(opts, "tools", pf.Source)
	if tools == nil || tools.Type() != "array" {
		if tools != nil {
			a.Opaque = true
		}
		return
	}
	for i := 0; i < int(tools.NamedChildCount()); i++ {
		item := tools.NamedChild(i)
		switch item.Type() {
		case "call_expression":
			if _, ok := classifyTSOpenAIHostedFactoryCall(item, aliases, pf.Source, pf.RelPath); ok {
				canon := astutil.TSCalleeText(item, pf.Source, aliases)
				a.HostedToolRefs = append(a.HostedToolRefs, models.HostedToolRef{
					Class:    canon,
					DefIndex: -1,
				})
			} else {
				// An inline user-tool factory (tools: [tool({...})]) or any other
				// unrecognized call: discovery cannot wire it to a ToolDef edge by
				// symbol, so the agent's tool set is not fully enumerable. Mark
				// Opaque (the same signal used above when tools is not an array
				// literal) so "agent has no tools" rules don't false-fire on an
				// agent that does own a tool. TODO(v2): resolve the inline tool to
				// a precise edge by its call-site location.
				a.Opaque = true
			}
		case "new_expression":
			a.Opaque = true
		case "identifier":
			a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: astutil.NodeText(item, pf.Source)})
		}
	}
}

// populateTSOpenAIIdentifierList walks a named property whose value is an
// array of identifiers, invoking emit() for each identifier's text.
func populateTSOpenAIIdentifierList(opts *sitter.Node, key string, src []byte, emit func(string)) {
	arr := getObjectProperty(opts, key, src)
	if arr == nil || arr.Type() != "array" {
		return
	}
	for i := 0; i < int(arr.NamedChildCount()); i++ {
		item := arr.NamedChild(i)
		if item.Type() != "identifier" {
			continue
		}
		emit(astutil.NodeText(item, src))
	}
}
