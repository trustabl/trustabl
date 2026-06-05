package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Vercel AI SDK (TypeScript) agent discovery.
//
// Two agent shapes, both SDK=SDKVercelAI, Language=typescript:
//
//  1. Call-based: generateText / streamText / generateObject / streamObject
//     ({ model, system, tools, stopWhen, maxSteps, toolChoice }). An AgentDef
//     is emitted ONLY when the options object carries a `tools` property — a
//     bare generateText({model, prompt}) is a one-shot completion, not an
//     agent, and emitting one would flood findings. Class is the normalized
//     callee name ("GenerateText" / "StreamText" / "GenerateObject" /
//     "StreamObject").
//
//  2. Class-based: new ToolLoopAgent({...}) and new Experimental_Agent({...})
//     (often imported `as Agent`; alias-resolved via the `ai` import map).
//     Both normalize to Class "ToolLoopAgent". The system-prompt slot here is
//     `instructions` (vs `system` in the call form) — both keys are captured.
//
// THE NEW MECHANIC: in BOTH forms, `tools` is an OBJECT/RECORD
// (`{ weather: weatherTool, search: tool({...}) }`), NOT an array. Every other
// TS agent pass reads `tools: [...]` arrays; this walk iterates the object's
// property VALUES. For each value:
//   - a bare identifier        -> ToolRef{Name: ident}   (ResolveEdges wires it
//                                  by VarName/Name; Vercel ToolDefs have empty
//                                  Name + VarName set, and toolsByFileSym keys
//                                  by both, so it resolves).
//   - an inline tool({...}) /  -> mark the agent Opaque (no symbol edge), as
//     dynamicTool({...}) call      ts_openai_agents marks inline tool({...}).
//   - <provider>.tools.<name>()-> HostedToolRef{Class: canonical, DefIndex:-1}
//                                  for ResolveEdges to materialize.
//   - a spread (...mcpTools) or -> mark the agent Opaque.
//     any other value

// tsVercelAgentCallFactories maps a recognized generation-call callee to its
// normalized Class. An agent is emitted from one of these only when `tools` is
// present in the options object.
var tsVercelAgentCallFactories = map[string]string{
	"generateText":   "GenerateText",
	"streamText":     "StreamText",
	"generateObject": "GenerateObject",
	"streamObject":   "StreamObject",
}

// tsVercelAgentCtors maps a `new X({...})` constructor (resolved via the `ai`
// alias map) to its normalized Class. Both the stable ToolLoopAgent and the
// experimental alias normalize to "ToolLoopAgent".
var tsVercelAgentCtors = map[string]string{
	"ToolLoopAgent":      "ToolLoopAgent",
	"Experimental_Agent": "ToolLoopAgent",
}

// DiscoverTSVercelAgents walks each parsed TS file and emits an AgentDef per
// recognized Vercel agent shape. Import-gated to the `ai` module.
func DiscoverTSVercelAgents(files []ParsedFile, onFile func(string)) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSVercelAgentsInFile(pf)...)
	}
	return out
}

func discoverTSVercelAgentsInFile(pf ParsedFile) []models.AgentDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesMatch(pf.Tree.RootNode(), pf.Source, isTSVercelModule)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression":
			class, ok := tsVercelAgentCallFactories[astutil.TSCalleeText(n, pf.Source, aliases)]
			if !ok {
				return true
			}
			// Call-form agents require a tools record — a bare completion is
			// not an agent.
			opts := firstObjectArg(n)
			if opts == nil || getObjectProperty(opts, "tools", pf.Source) == nil {
				return true
			}
			out = append(out, buildTSVercelAgent(n, opts, pf, class))
		case "new_expression":
			ctor := n.ChildByFieldName("constructor")
			if ctor == nil || ctor.Type() != "identifier" {
				return true
			}
			class, ok := tsVercelAgentCtors[aliases[astutil.NodeText(ctor, pf.Source)]]
			if !ok {
				return true
			}
			args := n.ChildByFieldName("arguments")
			var opts *sitter.Node
			if args != nil && args.NamedChildCount() > 0 {
				opts = args.NamedChild(0)
			}
			out = append(out, buildTSVercelAgent(n, opts, pf, class))
		}
		return true
	})
	return out
}

// buildTSVercelAgent builds an AgentDef from a call or new-expression. opts is
// the already-extracted options object (may be nil for a class ctor with no
// args). class is the normalized Class.
func buildTSVercelAgent(n, opts *sitter.Node, pf ParsedFile, class string) models.AgentDef {
	a := models.AgentDef{
		SDK:      models.SDKVercelAI,
		Class:    class,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(n, pf.Source),
	}
	if opts == nil || opts.Type() != "object" {
		a.Opaque = true
		return a
	}
	// A spread at the top level means the captured kwarg view is incomplete.
	for i := 0; i < int(opts.NamedChildCount()); i++ {
		if opts.NamedChild(i).Type() == "spread_element" {
			a.Opaque = true
			break
		}
	}
	a.Kwargs = astutil.TSObjectKwargs(opts, pf.Source)
	populateTSVercelToolRefs(&a, opts, pf)
	return a
}

// populateTSVercelToolRefs walks options.tools as an OBJECT RECORD (not an
// array) and resolves each property value to a ToolRef, a HostedToolRef, or an
// Opaque flag. A non-object tools value (identifier, spread, array, computed)
// is unenumerable by symbol, so the agent is marked Opaque.
func populateTSVercelToolRefs(a *models.AgentDef, opts *sitter.Node, pf ParsedFile) {
	tools := getObjectProperty(opts, "tools", pf.Source)
	if tools == nil {
		return
	}
	if tools.Type() != "object" {
		// tools: someRecord / tools: [...] / tools: computed — can't enumerate.
		a.Opaque = true
		return
	}
	for i := 0; i < int(tools.NamedChildCount()); i++ {
		prop := tools.NamedChild(i)
		if prop.Type() == "spread_element" {
			// tools: { ...mcpTools } — unenumerable.
			a.Opaque = true
			continue
		}
		if prop.Type() != "pair" {
			continue // shorthand / computed key handled below as opaque only if a value-bearing pair
		}
		val := prop.ChildByFieldName("value")
		if val == nil {
			continue
		}
		switch val.Type() {
		case "identifier":
			a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: astutil.NodeText(val, pf.Source)})
		case "call_expression":
			if canon, ok := classifyTSVercelHostedCall(val, pf.Source); ok {
				a.HostedToolRefs = append(a.HostedToolRefs, models.HostedToolRef{
					Class:    canon,
					DefIndex: -1,
				})
			} else {
				// An inline tool({...}) / dynamicTool({...}) (or any other call):
				// no symbol edge to a ToolDef, so the agent's tool set is not
				// fully enumerable. Mark Opaque so "agent has no tools" rules do
				// not false-fire.
				a.Opaque = true
			}
		default:
			// A non-identifier, non-call value (member access, object, etc.)
			// cannot be wired to an edge — be conservative.
			a.Opaque = true
		}
	}
}

// classifyTSVercelHostedCall reports whether a call_expression is a Vercel
// provider hosted-tool call (`<provider>.tools.<name>(...)`) and returns its
// canonical class string. The callee is a member_expression whose text is read
// directly: the provider object (anthropic / openai / google) is a runtime
// value from a provider package, not an import alias TSCalleeText can resolve.
func classifyTSVercelHostedCall(call *sitter.Node, src []byte) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_expression" {
		return "", false
	}
	canon := canonicalVercelHostedClass(astutil.NodeText(fn, src))
	if !IsVercelHostedTool(canon) {
		return "", false
	}
	return canon, true
}
