package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsMCPSDKModules gates MCP-server-authoring discovery: only files importing
// the @modelcontextprotocol/sdk server entrypoints are processed. These are
// the v1.x subpath imports (the canonical ESM form). The v2 line
// (@modelcontextprotocol/server) is pre-alpha and not yet handled.
var tsMCPSDKModules = []string{
	"@modelcontextprotocol/sdk/server/mcp.js",
	"@modelcontextprotocol/sdk/server/index.js",
}

// tsMCPServerClasses are the constructors whose instances expose the tool
// registration methods. McpServer is the high-level API; Server is the
// low-level one (tools there are returned from a ListTools handler, not named
// at a call site, so we only use it to mark a server var as a receiver).
var tsMCPServerClasses = map[string]bool{"McpServer": true, "Server": true}

// DiscoverTSMCPProper walks each parsed TS file and emits a ToolDef per MCP
// tool registered on a server authored with @modelcontextprotocol/sdk —
// `server.registerTool(name, config, handler)` (current) and the legacy
// `server.tool(name, ...)` overloads.
//
// This is distinct from DiscoverTSMCPServers (ts_mcp_servers.go), which
// recognizes Claude Agent SDK *client-side* server configs
// (createSdkMcpServer / options.mcpServers) and emits MCPServerDef connection
// records — never a ToolDef. The two never alias: different import gate
// (@anthropic-ai/claude-agent-sdk vs @modelcontextprotocol/sdk), different
// output slice (inv.MCPServers vs inv.Tools).
//
// Receiver-aware: discovery first collects the variables bound to
// `new McpServer(...)` / `new Server(...)`, then only attributes
// `.registerTool` / `.tool` calls whose receiver is one of those variables.
// This prevents an unrelated `.tool(...)` (or a Claude `createSdkMcpServer`
// result imported in the same file) from being mis-discovered.
func DiscoverTSMCPProper(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSMCPProperInFile(pf)...)
	}
	return out
}

func discoverTSMCPProperInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsMCPSDKModules)
	if len(aliases) == 0 {
		return nil // import gate
	}

	// Pass 1: collect server variables bound to a server constructor.
	serverVars := map[string]bool{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "new_expression" {
			return true
		}
		ctor := n.ChildByFieldName("constructor")
		if ctor == nil || ctor.Type() != "identifier" {
			return true
		}
		if canon := aliases[astutil.NodeText(ctor, pf.Source)]; !tsMCPServerClasses[canon] {
			return true
		}
		if name := directAssignmentName(n, pf.Source); name != "" {
			serverVars[name] = true
		}
		return true
	})
	if len(serverVars) == 0 {
		return nil
	}

	// Pass 2: member calls <serverVar>.registerTool(...) / .tool(...).
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			return true
		}
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil || obj.Type() != "identifier" {
			return true
		}
		if !serverVars[astutil.NodeText(obj, pf.Source)] {
			return true
		}
		method := astutil.NodeText(prop, pf.Source)
		if method != "registerTool" && method != "tool" {
			return true
		}
		if td, ok := extractTSMCPTool(n, method, pf); ok {
			out = append(out, td)
		}
		return true
	})
	return out
}

func extractTSMCPTool(call *sitter.Node, method string, pf ParsedFile) (models.ToolDef, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return models.ToolDef{}, false
	}
	pos := positionalArgs(args)
	if len(pos) == 0 {
		return models.ToolDef{}, false
	}
	td := models.ToolDef{
		Name:     tsStringLiteralText(pos[0], pf.Source),
		Kind:     models.KindMCPTool,
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
		VarName: directAssignmentName(call, pf.Source),
	}

	// Handler is always the last positional argument in both forms.
	if len(pos) >= 2 {
		hc := tsHandlerCapture(pos[len(pos)-1], pf.Source)
		if len(hc.facts) > 0 {
			td.Facts = hc.facts
		}
		td.HTTPHosts = hc.httpHosts
		td.FSWritePaths = hc.fsWritePaths
		td.HTTPMethods = hc.httpMethods
		td.HTTPCalls = hc.httpCalls
	}

	switch method {
	case "registerTool":
		// registerTool(name, {title, description, inputSchema}, handler)
		if len(pos) >= 2 && pos[1].Type() == "object" {
			if d := getObjectProperty(pos[1], "description", pf.Source); d != nil {
				td.Description = tsStringLiteralText(d, pf.Source)
			}
			if sch := getObjectProperty(pos[1], "inputSchema", pf.Source); sch != nil {
				if names, typed := tsZodParamNames(sch, pf.Source); typed {
					td.HasTypedParams = true
					td.ParamNames = names
				}
			}
		}
	case "tool":
		// Legacy overloads:
		//   tool(name, handler)
		//   tool(name, paramsSchema, handler)
		//   tool(name, description, paramsSchema, handler)
		//   tool(name, description, paramsSchema, annotations, handler)
		// description = first string-literal among the middle args;
		// schema = first object among the middle args (not the handler).
		for i := 1; i < len(pos)-1; i++ {
			a := pos[i]
			if a.Type() == "string" && td.Description == "" {
				td.Description = tsStringLiteralText(a, pf.Source)
				continue
			}
			if a.Type() == "object" && !td.HasTypedParams {
				if names, typed := tsZodParamNames(a, pf.Source); typed {
					td.HasTypedParams = true
					td.ParamNames = names
				}
			}
		}
	}
	return td, true
}
