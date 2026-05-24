package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverTSMCPServers extracts MCPServerDef records from TS source. Two
// recognition paths:
//   1. createSdkMcpServer({...}) calls (SDK-instance servers)
//   2. Object literals with type: "stdio"|"sse"|"http"|"sdk" inside
//      options.mcpServers records (added in a later task)
func DiscoverTSMCPServers(files []ParsedFile, onFile func(string)) []models.MCPServerDef {
	var out []models.MCPServerDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSMCPServersInFile(pf)...)
	}
	return out
}

func discoverTSMCPServersInFile(pf ParsedFile) []models.MCPServerDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliases(pf.Tree.RootNode(), pf.Source, tsClaudeSDKModule)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.MCPServerDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		callee := astutil.TSCalleeText(n, pf.Source, aliases)
		switch callee {
		case "createSdkMcpServer":
			out = append(out, models.MCPServerDef{
				Class:     "createSdkMcpServer",
				Transport: "sdk",
				SDK:       models.SDKClaudeAgentSDK,
				Language:  models.LanguageTypeScript,
				FilePath:  pf.RelPath,
				Line:      int(n.StartPoint().Row) + 1,
			})
		case "query":
			out = append(out, extractMCPConfigsFromQuery(n, pf)...)
		}
		return true
	})
	return out
}

// extractMCPConfigsFromQuery drills into query({options: {mcpServers: {...}}})
// and emits one MCPServerDef per object-literal value, discriminating by
// the value's "type" field.
func extractMCPConfigsFromQuery(call *sitter.Node, pf ParsedFile) []models.MCPServerDef {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 1 {
		return nil
	}
	root := args.NamedChild(0)
	if root.Type() != "object" {
		return nil
	}
	options := getObjectProperty(root, "options", pf.Source)
	if options == nil || options.Type() != "object" {
		return nil
	}
	servers := getObjectProperty(options, "mcpServers", pf.Source)
	if servers == nil || servers.Type() != "object" {
		return nil
	}
	var out []models.MCPServerDef
	for i := 0; i < int(servers.NamedChildCount()); i++ {
		prop := servers.NamedChild(i)
		if prop.Type() != "pair" {
			continue
		}
		val := prop.ChildByFieldName("value")
		if val == nil || val.Type() != "object" {
			continue // identifier-valued entries handled in Task 22
		}
		typeNode := getObjectProperty(val, "type", pf.Source)
		if typeNode == nil || typeNode.Type() != "string" {
			continue
		}
		raw := astutil.NodeText(typeNode, pf.Source)
		if len(raw) < 2 {
			continue
		}
		transport := raw[1 : len(raw)-1]
		class := tsMCPClassForTransport(transport)
		if class == "" {
			continue
		}
		out = append(out, models.MCPServerDef{
			Class:     class,
			Transport: transport,
			SDK:       models.SDKClaudeAgentSDK,
			Language:  models.LanguageTypeScript,
			FilePath:  pf.RelPath,
			Line:      int(val.StartPoint().Row) + 1,
			Kwargs:    astutil.TSObjectKwargs(val, pf.Source),
		})
	}
	return out
}

func tsMCPClassForTransport(t string) string {
	switch t {
	case "stdio":
		return "McpStdioServerConfig"
	case "sse":
		return "McpSSEServerConfig"
	case "http":
		return "McpHttpServerConfig"
	case "sdk":
		return "McpSdkServerConfigWithInstance"
	}
	return ""
}
