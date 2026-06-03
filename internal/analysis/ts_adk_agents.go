package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// TSADKAgentClasses is the closed set of @google/adk agent constructor
// names recognized by TS discovery. Set semantics — no normalization
// happens (unlike Python's adkAgentClasses which maps "Agent" →
// "LlmAgent"; ADK JS has no such alias).
var TSADKAgentClasses = map[string]bool{
	"LlmAgent":        true,
	"SequentialAgent": true,
	"ParallelAgent":   true,
	"LoopAgent":       true,
	"RoutedAgent":     true,
}

// DiscoverTSADKAgents walks each parsed TS file and emits an AgentDef per
// recognized @google/adk agent constructor call, import-gated to
// @google/adk.
//
// HostedToolRefs are pre-resolved at discovery time using the file-local
// alias map (threading it into ResolveEdges would be a bigger refactor).
// Tool identifier refs, sub-agent refs, etc. are left for ResolveEdges to
// wire by binding name.
func DiscoverTSADKAgents(files []ParsedFile, onFile func(string)) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSADKAgentsInFile(pf)...)
	}
	return out
}

func discoverTSADKAgentsInFile(pf ParsedFile) []models.AgentDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsADKModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "new_expression" {
			return true
		}
		// v1 limitation: only bare-identifier constructors are recognized.
		// Namespace-import constructors like `new ns.LlmAgent({...})`
		// (a member_expression) are not handled.
		ctor := n.ChildByFieldName("constructor")
		if ctor == nil || ctor.Type() != "identifier" {
			return true
		}
		canon, ok := aliases[astutil.NodeText(ctor, pf.Source)]
		if !ok || !TSADKAgentClasses[canon] {
			return true
		}
		out = append(out, buildTSADKAgent(n, pf, aliases, canon))
		return true
	})
	return out
}

// buildTSADKAgent constructs an AgentDef from a validated new_expression.
// The caller has already confirmed the constructor resolves to a known
// ADK agent class (canon is the canonical class name).
func buildTSADKAgent(n *sitter.Node, pf ParsedFile, aliases map[string]string, canon string) models.AgentDef {
	a := models.AgentDef{
		SDK:      models.SDKGoogleADK,
		Class:    canon,
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
	populateTSADKToolRefs(&a, opts, pf, aliases)
	populateTSADKIdentifierList(opts, "subAgents", pf.Source, func(name string) {
		a.HandoffRefs = append(a.HandoffRefs, models.AgentRef{Name: name})
	})
	return a
}

// populateTSADKToolRefs walks options.tools (must be an array) and for each
// item:
//   - new_expression whose alias-resolved constructor is a hosted-tool class →
//     emit a HostedToolRef on the agent
//   - identifier → emit a ToolRef{Name: identifier text} for ResolveEdges
//
// Items of any other kind (spread, computed expr, unrecognized class) are
// skipped silently.
func populateTSADKToolRefs(a *models.AgentDef, opts *sitter.Node, pf ParsedFile, aliases map[string]string) {
	tools := getObjectProperty(opts, "tools", pf.Source)
	if tools == nil || tools.Type() != "array" {
		if tools != nil {
			// non-array tools= — mark opaque so rules don't trust ToolRefs
			a.Opaque = true
		}
		return
	}
	for i := 0; i < int(tools.NamedChildCount()); i++ {
		item := tools.NamedChild(i)
		switch item.Type() {
		case "new_expression":
			if def, ok := classifyTSADKHostedNewExpression(item, aliases, pf.Source, pf.RelPath); ok {
				a.HostedToolRefs = append(a.HostedToolRefs, models.HostedToolRef{
					Class:    def.Class,
					DefIndex: -1, // appended to inv.HostedTools by ResolveEdges
				})
			} else {
				// Inline `new FunctionTool({...})` or any other unrecognized
				// constructor: cannot be wired to a ToolDef edge by symbol, so the
				// tool set is not fully enumerable. Mark Opaque (same signal as the
				// non-array tools case above) so "agent has no tools" rules don't
				// false-fire. TODO(v2): resolve the inline tool by call-site.
				a.Opaque = true
			}
		case "call_expression":
			a.Opaque = true
		case "identifier":
			a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: astutil.NodeText(item, pf.Source)})
		}
	}
}

// populateTSADKIdentifierList walks a named property whose value is an array
// of identifiers, invoking emit() for each identifier's text.
func populateTSADKIdentifierList(opts *sitter.Node, key string, src []byte, emit func(string)) {
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
