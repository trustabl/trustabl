package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Agent execution-call discovery: OpenAI Agents SDK and Pydantic AI.
//
// Execution limits (max_turns for OpenAI Agents SDK, usage_limits for
// Pydantic AI) are set on the call that RUNS an agent, not on the agent's
// constructor — Runner.run(agent, ..., max_turns=N) / agent.run(...,
// usage_limits=UsageLimits(...)). Neither is visible to DiscoverAgents /
// DiscoverPydanticAIAgents, which only capture the constructor call. This
// file adds a second, independent discovery pass over the same call-node
// shape so agent-scope rules can correlate a specific AgentDef to the call
// that executes it (by same-file VarName), mirroring how
// DiscoverClaudeAgentOptions captures ClaudeAgentOptions(...) constructions
// separately from agent discovery.
//
// The two SDKs need different discriminators:
//
//   - OpenAI Agents SDK: Runner is a fixed class (imported from `agents`),
//     so the callee is matched by requiring the attribute's object be
//     exactly "Runner" or end in ".Runner" (e.g. `agents.Runner.run(...)`).
//     Matching on a suffix of the whole callee text (e.g. "Runner.run")
//     would false-positive on any unrelated class merely named *Runner
//     (TaskRunner.run(), TestRunner.run(), ...) — the object segment must
//     match exactly, not just the tail of the dotted string. The agent
//     being run is the call's first positional argument (mirrors
//     Runner.run(starting_agent, input, ...)).
//
//   - Pydantic AI: `<agent>.run(...)` is a bare method call on an
//     arbitrary local variable — there is no fixed class name to match, so
//     the agent being run is the method's RECEIVER, not a positional
//     argument (unlike OpenAI). Over-capturing here is safe: a call whose
//     receiver name doesn't correspond to any discovered PydanticAgent
//     VarName in the same file simply never correlates in a downstream
//     predicate (same same-file-VarName-only limitation ClaudeAgentOptions
//     and other call-capture discoverers already accept). The file-level
//     `fileImportsPydanticAI` gate keeps this from firing in files that
//     never touch Pydantic AI at all.
//
// Both discoverers require the first positional arg (OpenAI) / receiver
// (Pydantic AI) to be a plain identifier, silently skipping anything else
// (a call expression, an attribute access, a keyword arg), mirroring
// adk_agents.go's DiscoverADKTools.

// openAIRunnerMethods is the closed set of Runner class methods that execute
// an agent and accept max_turns.
var openAIRunnerMethods = map[string]bool{
	"run": true, "run_sync": true, "run_streamed": true,
}

// pydanticAIRunMethods is the closed set of Agent instance methods that
// execute an agent and accept usage_limits.
var pydanticAIRunMethods = map[string]bool{
	"run": true, "run_sync": true, "run_stream": true,
}

// DiscoverAgentRunCalls walks each ParsedFile and emits one AgentRunCallDef
// per recognized Runner.run-family call (OpenAI Agents SDK) or
// <agent>.run-family call (Pydantic AI). Purely additive: it does not modify
// or depend on the AgentDef list produced by DiscoverAgents /
// DiscoverPydanticAIAgents, only on the same-file VarName convention they
// already populate.
func DiscoverAgentRunCalls(files []ParsedFile) []models.AgentRunCallDef {
	var out []models.AgentRunCallDef
	for _, pf := range files {
		importsOpenAI := fileImportsOpenAIAgentsSDK(pf)
		importsPydantic := fileImportsPydanticAI(pf)
		if !importsOpenAI && !importsPydantic {
			continue
		}
		out = append(out, discoverAgentRunCallsInFile(pf, importsOpenAI, importsPydantic)...)
	}
	return out
}

// isOpenAIAgentsModule reports whether a dotted module path belongs to the
// OpenAI Agents SDK: `agents`, `agents.*`. Mirrors isPydanticAIModule.
func isOpenAIAgentsModule(mod string) bool {
	return mod == "agents" || strings.HasPrefix(mod, "agents.")
}

// fileImportsOpenAIAgentsSDK reports whether pf imports the OpenAI Agents SDK
// via a real import statement (AST-based, not a source substring).
func fileImportsOpenAIAgentsSDK(pf ParsedFile) bool {
	return fileImportsModule(pf, isOpenAIAgentsModule)
}

func discoverAgentRunCallsInFile(pf ParsedFile, importsOpenAI, importsPydantic bool) []models.AgentRunCallDef {
	var out []models.AgentRunCallDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" {
			return true
		}
		attr := fn.ChildByFieldName("attribute")
		obj := fn.ChildByFieldName("object")
		if attr == nil || obj == nil {
			return true
		}
		method := astutil.NodeText(attr, pf.Source)

		if importsOpenAI && openAIRunnerMethods[method] && isRunnerObject(obj, pf) {
			if rc, ok := buildOpenAIRunCall(n, fn, pf); ok {
				out = append(out, rc)
			}
			return true
		}
		if importsPydantic && pydanticAIRunMethods[method] && obj.Type() == "identifier" {
			out = append(out, buildPydanticRunCall(n, obj, method, pf))
		}
		return true
	})
	return out
}

// isRunnerObject reports whether the attribute's object segment is exactly
// "Runner" or a dotted-qualified access ending in ".Runner" (e.g.
// `agents.Runner`). Deliberately NOT a suffix check on the whole callee text
// — that would also match an unrelated TaskRunner.run() / TestRunner.run().
func isRunnerObject(obj *sitter.Node, pf ParsedFile) bool {
	text := astutil.NodeText(obj, pf.Source)
	return text == "Runner" || strings.HasSuffix(text, ".Runner")
}

func buildOpenAIRunCall(n, fn *sitter.Node, pf ParsedFile) (models.AgentRunCallDef, bool) {
	args := n.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return models.AgentRunCallDef{}, false
	}
	first := args.NamedChild(0)
	// Only a positional identifier is resolvable (e.g. Runner.run(my_agent, ...)).
	// Anything else (Runner.run(get_agent()), Runner.run(agent=my_agent)) is
	// silently skipped — those cannot be correlated to an AgentDef by name.
	if first.Type() != "identifier" {
		return models.AgentRunCallDef{}, false
	}
	kwargs, opaque := extractCallKwargs(n, pf.Source)
	return models.AgentRunCallDef{
		SDK:          models.SDKOpenAIAgents,
		Callee:       astutil.NodeText(fn, pf.Source),
		AgentVarName: astutil.NodeText(first, pf.Source),
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		Kwargs: kwargs,
		Opaque: opaque,
	}, true
}

func buildPydanticRunCall(n, obj *sitter.Node, method string, pf ParsedFile) models.AgentRunCallDef {
	kwargs, opaque := extractCallKwargs(n, pf.Source)
	return models.AgentRunCallDef{
		SDK:          models.SDKPydanticAI,
		Callee:       astutil.NodeText(obj, pf.Source) + "." + method,
		AgentVarName: astutil.NodeText(obj, pf.Source),
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		Kwargs: kwargs,
		Opaque: opaque,
	}
}
