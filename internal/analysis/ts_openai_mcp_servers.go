package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsOpenAIMCPClasses maps the four recognized OpenAI MCP server class names
// to their transport label. "multi" is a Trustabl convention for the
// MCPServers wrapper (not a real transport — the wrapper holds N servers).
var tsOpenAIMCPClasses = map[string]string{
	"MCPServerStdio":          "stdio",
	"MCPServerSSE":            "sse",
	"MCPServerStreamableHttp": "streamable_http",
	"MCPServers":              "multi",
}

// DiscoverTSOpenAIMCPServers walks each parsed TS file and emits an
// MCPServerDef for every `new <McpClass>({...})` expression where <McpClass>
// resolves via aliases to one of the @openai/agents MCP server classes.
func DiscoverTSOpenAIMCPServers(files []ParsedFile, onFile func(string)) []models.MCPServerDef {
	var out []models.MCPServerDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSOpenAIMCPServersInFile(pf)...)
	}
	return out
}

func discoverTSOpenAIMCPServersInFile(pf ParsedFile) []models.MCPServerDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsOpenAIAgentsModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.MCPServerDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "new_expression" {
			return true
		}
		ctor := n.ChildByFieldName("constructor")
		if ctor == nil || ctor.Type() != "identifier" {
			return true
		}
		local := astutil.NodeText(ctor, pf.Source)
		canon, ok := aliases[local]
		if !ok {
			return true
		}
		transport, recognized := tsOpenAIMCPClasses[canon]
		if !recognized {
			return true
		}
		def := models.MCPServerDef{
			Class:     canon,
			Transport: transport,
			SDK:       models.SDKOpenAIAgents,
			Language:  models.LanguageTypeScript,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			VarName: directAssignmentName(n, pf.Source),
		}
		// Capture kwargs only when arg 0 is an object literal.
		if args := n.ChildByFieldName("arguments"); args != nil && args.NamedChildCount() > 0 {
			if arg0 := args.NamedChild(0); arg0.Type() == "object" {
				def.Kwargs = astutil.TSObjectKwargs(arg0, pf.Source)
			}
		}
		out = append(out, def)
		return true
	})
	return out
}
