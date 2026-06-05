package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// AutoGen / AG2 Python agent discovery.
//
// AutoGen ships across two upstream lines that share class names but live in
// different import roots:
//
//   - The AG2 / 0.2 line (`autogen`, formerly `pyautogen`) exports
//     ConversableAgent / UserProxyAgent / AssistantAgent / GroupChat /
//     GroupChatManager.
//   - Microsoft's v0.4 line (`autogen_agentchat`, `autogen_core`, `autogen_ext`)
//     exports AssistantAgent and CodeExecutorAgent.
//
// Several of these class names (AssistantAgent, GroupChat) collide with other
// SDKs and with user code, so all discovery is import-gated to files that import
// either AutoGen line (fileImportsAutoGen). The emitted AgentDef carries
// SDK=SDKAutoGen and Class set to the exact constructor name;
// agentKindMatches keys each applies_to token on BOTH SDK==SDKAutoGen AND the
// class name, so a collision never produces a cross-SDK match.
//
// The executor-class hosted discovery (CodeExecutorAgent +
// LocalCommandLineCodeExecutor) and the register_function caller/executor
// two-agent edge are documented v1 gaps.

// autoGenAgentClasses is the closed set of AutoGen agent constructors across both
// upstream lines, mapped to the canonical class name stamped on AgentDef.Class.
// GroupChat is a config object rather than a runtime agent, but it carries the
// max_round speaker-loop bound that AG2-004 audits, so it is discovered and
// agentKindMatches("autogen_group_chat_manager") accepts both it and
// GroupChatManager.
var autoGenAgentClasses = map[string]string{
	"ConversableAgent":  "ConversableAgent",
	"UserProxyAgent":    "UserProxyAgent",
	"AssistantAgent":    "AssistantAgent",
	"GroupChat":         "GroupChat",
	"GroupChatManager":  "GroupChatManager",
	"CodeExecutorAgent": "CodeExecutorAgent",
}

// isAutoGenAG2Module reports whether a dotted module path belongs to the AG2 /
// 0.2 line: `autogen` or `autogen.*`. The dot boundary is load-bearing — it must
// NOT match the v0.4 packages `autogen_agentchat` / `autogen_core` /
// `autogen_ext`, which are a separate import line handled by
// isAutoGenV04Module. (`pyautogen` and `ag2` are the *distribution* names on
// PyPI; the imported module is still `autogen`, so only `autogen` is matched.)
func isAutoGenAG2Module(mod string) bool {
	return mod == "autogen" || strings.HasPrefix(mod, "autogen.")
}

// isAutoGenV04Module reports whether a dotted module path belongs to Microsoft's
// v0.4 line: autogen_agentchat / autogen_core / autogen_ext and their dotted
// submodules. The underscore-prefixed roots are deliberately distinct from the
// AG2 `autogen` root so the two lines never alias each other.
func isAutoGenV04Module(mod string) bool {
	for _, root := range []string{"autogen_agentchat", "autogen_core", "autogen_ext"} {
		if mod == root || strings.HasPrefix(mod, root+".") {
			return true
		}
	}
	return false
}

// fileImportsAutoGen reports whether pf imports EITHER AutoGen upstream line via
// a real import statement (AST-based, not a source substring — a comment that
// merely mentions autogen must not trip the gate). The union of the two lines is
// the discovery gate.
func fileImportsAutoGen(pf ParsedFile) bool {
	return fileImportsModule(pf, func(mod string) bool {
		return isAutoGenAG2Module(mod) || isAutoGenV04Module(mod)
	})
}

// DiscoverAutoGenAgents walks each ParsedFile and emits one AgentDef per
// recognized AutoGen agent constructor call. Only files importing an AutoGen
// line are considered (the import gate disambiguates AssistantAgent / GroupChat
// from other SDKs and user code of the same name).
func DiscoverAutoGenAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsAutoGen(pf) {
			continue
		}
		out = append(out, discoverAutoGenAgentsInFile(pf)...)
	}
	return out
}

func discoverAutoGenAgentsInFile(pf ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		class, ok := autoGenAgentClasses[callee]
		if !ok {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKAutoGen,
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
		// AutoGen agents are labeled by a name= string literal; capture it.
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		}
		// Capture the assignment-target identifier (user_proxy = UserProxyAgent(...))
		// so a GroupChat's agents=[...] and register_function caller/executor
		// references can resolve by variable name in a future pass.
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
