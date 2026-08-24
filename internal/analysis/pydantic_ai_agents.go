package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Pydantic AI Python agent discovery.
//
// Pydantic AI declares an agent with a single `Agent(model, output_type=,
// system_prompt=, instructions=, tools=, retries=, end_strategy=, ...)`
// constructor. The class name `Agent` collides with the OpenAI Agents SDK,
// Google ADK, and CrewAI's `Agent`, so all discovery is import-gated to files
// that import `pydantic_ai`. The emitted AgentDef carries SDK=SDKPydanticAI and
// the normalized Class "PydanticAgent" (the upstream class is `Agent`, but the
// stamped name disambiguates it from the other SDKs' `Agent`);
// agentKindMatches("pydantic_ai_agent") keys on BOTH SDK and Class, so a
// Pydantic Agent and an OpenAI/ADK/CrewAI Agent never cross-match even though
// the source token is identical.
//
// Pydantic TOOLS are the `@<agentvar>.tool` / `@<agentvar>.tool_plain` attribute
// decorators (routed through discovery.go's kindFromDecorators, import-gated via
// fileImportsPydanticAI / !fileImportsClaudeSDK) and the `Tool(fn, ...)` factory
// (pydantic_ai_tools.go). The bare-function `tools=[fn]` ToolDef-synthesis shape
// is a documented v1 gap (the agent edge still works via the tools= kwarg).

// pydanticAIAgentClasses is the closed set of Pydantic AI agent constructors.
// Pydantic AI has a single agent class. The source token is `Agent`; the value
// is the normalized Class name stamped on AgentDef.Class.
var pydanticAIAgentClasses = map[string]string{
	"Agent": "PydanticAgent",
}

// isPydanticAIModule reports whether a dotted module path belongs to the
// Pydantic AI ecosystem: `pydantic_ai`, `pydantic_ai.*`. The dot boundary keeps
// an unrelated package that merely shares the prefix text (e.g. "pydantic_aix")
// from matching, mirroring isCrewAIModule.
func isPydanticAIModule(mod string) bool {
	return mod == "pydantic_ai" || strings.HasPrefix(mod, "pydantic_ai.")
}

// fileImportsPydanticAI reports whether pf imports the Pydantic AI ecosystem via
// a real import statement (AST-based, not a source substring — a comment that
// merely mentions pydantic_ai must not trip the gate).
func fileImportsPydanticAI(pf ParsedFile) bool {
	return fileImportsModule(pf, isPydanticAIModule)
}

// DiscoverPydanticAIAgents walks each ParsedFile and emits one AgentDef per
// recognized Pydantic AI Agent(...) constructor call. Only files importing
// pydantic_ai are considered (the import gate disambiguates `Agent` from the
// OpenAI, ADK, and CrewAI classes of the same name).
func DiscoverPydanticAIAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsPydanticAI(pf) {
			continue
		}
		out = append(out, discoverPydanticAIAgentsInFile(pf)...)
	}
	return out
}

func discoverPydanticAIAgentsInFile(pf ParsedFile) []models.AgentDef {
	// One file-level walk: PYD-104 attributes FileUrl.force_download to every
	// Pydantic agent declared in the same file (a multi-agent file
	// over-attributes — the same v1 limit LangGraph's file-level graph tools
	// already accept).
	fileURLForce := pydanticFileURLForceDownload(pf)
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		class, ok := pydanticAIAgentClasses[callee]
		if !ok {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKPydanticAI,
			Class:    class,
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			Kwargs:               kwargs,
			Opaque:               opaque,
			FileURLForceDownload: fileURLForce,
		}
		// Pydantic agents may carry a name= string literal; capture it as the
		// human-facing label.
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		}
		// Capture the assignment-target identifier (agent = Agent(...)) so the
		// @agent.tool decorator owner and tools=[...] references resolve by
		// variable name.
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

// pydanticFileURLClasses is the closed set of Pydantic AI FileUrl subclasses
// that accept force_download. FileUrl itself is abstract but included so a
// rare direct/aliased construction still matches. calleeName strips a module
// qualifier (pydantic_ai.DocumentUrl -> DocumentUrl).
var pydanticFileURLClasses = map[string]bool{
	"FileUrl":     true,
	"ImageUrl":    true,
	"AudioUrl":    true,
	"VideoUrl":    true,
	"DocumentUrl": true,
}

// pydanticFileURLForceDownload walks pf for FileUrl-family constructors and
// returns the most-dangerous force_download value observed: "allow-local"
// outranks "True". Empty string means no constructor set a non-default value
// (absent or False). 'allow-local' lets the host fetch private IPs, bypassing
// Pydantic AI's SSRF guard; True still downloads on the host (with the
// private-IP blocklist that has already needed CVE fixes).
func pydanticFileURLForceDownload(pf ParsedFile) string {
	worst := ""
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		name := calleeName(astutil.NodeText(fn, pf.Source))
		if !pydanticFileURLClasses[name] {
			return true
		}
		val, present := astutil.KwargValue(n, pf.Source, "force_download")
		if !present {
			return true
		}
		norm := normalizeForceDownload(val)
		switch {
		case norm == "allow-local":
			worst = "allow-local"
		case (norm == "True" || norm == "true") && worst == "":
			worst = "True"
		}
		return true
	})
	return worst
}

// normalizeForceDownload strips quotes from a force_download kwarg's source
// text so '"allow-local"' and 'allow-local' compare equal.
func normalizeForceDownload(val string) string {
	return strings.Trim(strings.TrimSpace(val), `"'`)
}
