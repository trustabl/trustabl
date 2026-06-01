package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverTSAgents extracts AgentDef records from TS source. Two shapes:
//  1. Inline inside query({ options: { agents: { ... } } })
//  2. Typed-const declarations: const x: AgentDefinition = {...}
//
// (Typed-const shape added in a later task.)
func DiscoverTSAgents(files []ParsedFile, onFile func(string)) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSAgentsInFile(pf)...)
	}
	return out
}

func discoverTSAgentsInFile(pf ParsedFile) []models.AgentDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliases(pf.Tree.RootNode(), pf.Source, tsClaudeSDKModule)
	if len(aliases) == 0 {
		return nil // import gate
	}
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression":
			if astutil.TSCalleeText(n, pf.Source, aliases) == "query" {
				// Emit ONE QueryMainAgent for the query() call itself —
				// the TS SDK has no AgentDefinition for the main thread,
				// so the call site IS the agent's declaration. Then also
				// emit any inline-in-options.agents sub-agents.
				out = append(out, extractQueryMainAgent(n, pf))
				out = append(out, extractInlineAgentsFromQuery(n, pf)...)
			}
		case "variable_declarator":
			if a, ok := extractTypedConstAgent(n, pf); ok {
				out = append(out, a)
			}
		}
		return true
	})
	return out
}

// extractQueryMainAgent emits one AgentDef representing the main thread of
// a query() call. The TS Claude Agent SDK does not provide an AgentDefinition
// constructor for the main thread — the query({prompt, options}) call IS the
// main agent's declaration. Class is "QueryMainAgent" so SP2 rules can target
// this distinct shape (vs the "AgentDefinition" sub-agent shape).
//
// Opaque=true when arg 0 (or its options field) is not an inline object
// literal, e.g. query(getOptions()) or query({prompt, options: computed}).
// Real-world TS Claude SDK code commonly builds options via spread / class
// fields, so opaque is the common case.
func extractQueryMainAgent(call *sitter.Node, pf ParsedFile) models.AgentDef {
	name := extractQueryAssignmentName(call, pf.Source)
	agent := models.AgentDef{
		SDK:      models.SDKClaudeAgentSDK,
		Class:    "QueryMainAgent",
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
		Name:    name,
		VarName: name,
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 1 {
		agent.Opaque = true
		return agent
	}
	root := args.NamedChild(0)
	if root.Type() != "object" {
		// query(getOptions()) — arg 0 itself is a call/identifier/spread.
		agent.Opaque = true
		return agent
	}
	// Capture root as Kwargs — gives rules visibility into root.prompt plus
	// root.options.* (as a nested KwargTree) when both are inline.
	agent.Kwargs = astutil.TSObjectKwargs(root, pf.Source)
	options := getObjectProperty(root, "options", pf.Source)
	if options == nil {
		// query({prompt: "..."}) with no options block — that's fine, the
		// main agent uses SDK defaults. Not opaque; just no options to read.
		return agent
	}
	if options.Type() != "object" {
		// Common real-world case: query({prompt, options: mergedOptions}).
		// We can read prompt at root but options.* is hidden. Fall back to
		// a file-scoped scan for allowedTools so the agent's permission
		// surface isn't invisible.
		agent.Opaque = true
		populateTSMainAgentToolRefsFromFile(&agent, pf)
		return agent
	}
	// Inline options — extract ToolRefs from options.allowedTools (note: the
	// main agent uses "allowedTools", not "tools"; the latter is the
	// AgentDefinition sub-agent field) and MCPServerRefs from
	// options.mcpServers.
	populateTSMainAgentToolRefs(&agent, options, pf.Source)
	agent.MCPServerRefs = extractTSMCPServerRefs(options, pf)
	return agent
}

// extractQueryAssignmentName returns a useful identifier for a query() main
// agent. Tries in order:
//  1. Immediate `const X = query(...)` (or let/var) binding → "X".
//  2. Enclosing function/method declaration → "fn" or "Class.method".
//  3. "" if neither applies (e.g. top-level for-await-of in module scope).
//
// Walks through wrapping parenthesized_expression for case 1. For case 2,
// walks up the AST stopping at the first function_declaration,
// generator_function_declaration, or method_definition. For method_definition,
// prepends the enclosing class name when one exists.
func extractQueryAssignmentName(call *sitter.Node, src []byte) string {
	// Case 1: `const X = query(...)`.
	if name := directAssignmentName(call, src); name != "" {
		return name
	}
	// Case 2: enclosing named function/method.
	return enclosingFunctionName(call, src)
}

func directAssignmentName(call *sitter.Node, src []byte) string {
	parent := call.Parent()
	for parent != nil && parent.Type() == "parenthesized_expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Type() != "variable_declarator" {
		return ""
	}
	nameNode := parent.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "identifier" {
		return ""
	}
	return astutil.NodeText(nameNode, src)
}

func enclosingFunctionName(node *sitter.Node, src []byte) string {
	for cur := node.Parent(); cur != nil; cur = cur.Parent() {
		switch cur.Type() {
		case "function_declaration", "generator_function_declaration":
			n := cur.ChildByFieldName("name")
			if n != nil {
				return astutil.NodeText(n, src)
			}
			return ""
		case "method_definition":
			n := cur.ChildByFieldName("name")
			if n == nil {
				return ""
			}
			mname := astutil.NodeText(n, src)
			if cname := enclosingClassName(cur, src); cname != "" {
				return cname + "." + mname
			}
			return mname
		}
	}
	return ""
}

func enclosingClassName(node *sitter.Node, src []byte) string {
	for cur := node.Parent(); cur != nil; cur = cur.Parent() {
		if cur.Type() == "class_declaration" {
			n := cur.ChildByFieldName("name")
			if n != nil {
				return astutil.NodeText(n, src)
			}
			return ""
		}
	}
	return ""
}

// populateTSMainAgentToolRefs reads options.allowedTools (a list of string
// literals naming Claude Code builtins) and appends one ToolRef per entry.
// The main agent uses "allowedTools"; sub-agent AgentDefinitions use "tools"
// (handled by populateTSAgentToolRefs).
func populateTSMainAgentToolRefs(a *models.AgentDef, options *sitter.Node, src []byte) {
	toolsNode := getObjectProperty(options, "allowedTools", src)
	collectStringArrayIntoToolRefs(a, toolsNode, src)
}

// populateTSMainAgentToolRefsFromFile is the opaque-options fallback. The
// common real-world shape stores options in a class field or named const,
// then references it via identifier at the query() call site (e.g.
// `query({prompt, options: this.defaultOptions})`). When that happens we
// can't read options.allowedTools from the call site, but the array is
// usually a literal SOMEWHERE in the same file. Scan for any
// `allowedTools: [<string-literals>]` pair anywhere in the file and union
// the strings into the agent's ToolRefs. Heuristic — may over-extract if
// the file has multiple unrelated allowedTools arrays, but that's rare in
// real TS Claude SDK code.
func populateTSMainAgentToolRefsFromFile(a *models.AgentDef, pf ParsedFile) {
	if pf.Tree == nil {
		return
	}
	seen := map[string]bool{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "pair" {
			return true
		}
		k := n.ChildByFieldName("key")
		v := n.ChildByFieldName("value")
		if k == nil || v == nil {
			return true
		}
		var keyName string
		switch k.Type() {
		case "property_identifier":
			keyName = astutil.NodeText(k, pf.Source)
		case "string":
			raw := astutil.NodeText(k, pf.Source)
			if len(raw) >= 2 {
				keyName = raw[1 : len(raw)-1]
			}
		}
		if keyName != "allowedTools" || v.Type() != "array" {
			return true
		}
		for i := 0; i < int(v.NamedChildCount()); i++ {
			item := v.NamedChild(i)
			if item.Type() != "string" {
				continue
			}
			raw := astutil.NodeText(item, pf.Source)
			if len(raw) < 2 {
				continue
			}
			name := raw[1 : len(raw)-1]
			if seen[name] {
				continue
			}
			seen[name] = true
			a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: name})
		}
		return true
	})
}

// collectStringArrayIntoToolRefs appends one ToolRef per string-literal item
// in an array node. Used by the inline-options path.
func collectStringArrayIntoToolRefs(a *models.AgentDef, arr *sitter.Node, src []byte) {
	if arr == nil || arr.Type() != "array" {
		return
	}
	for i := 0; i < int(arr.NamedChildCount()); i++ {
		item := arr.NamedChild(i)
		if item.Type() != "string" {
			continue
		}
		raw := astutil.NodeText(item, src)
		if len(raw) < 2 {
			continue
		}
		a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: raw[1 : len(raw)-1]})
	}
}

func extractInlineAgentsFromQuery(call *sitter.Node, pf ParsedFile) []models.AgentDef {
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
	mcpRefs := extractTSMCPServerRefs(options, pf)

	agentsObj := getObjectProperty(options, "agents", pf.Source)
	if agentsObj == nil || agentsObj.Type() != "object" {
		return nil
	}
	var out []models.AgentDef
	for i := 0; i < int(agentsObj.NamedChildCount()); i++ {
		prop := agentsObj.NamedChild(i)
		if prop.Type() != "pair" {
			continue
		}
		keyNode := prop.ChildByFieldName("key")
		valNode := prop.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		var name string
		switch keyNode.Type() {
		case "property_identifier":
			name = astutil.NodeText(keyNode, pf.Source)
		case "string":
			raw := astutil.NodeText(keyNode, pf.Source)
			if len(raw) >= 2 {
				name = raw[1 : len(raw)-1]
			}
		}
		agent := models.AgentDef{
			SDK:      models.SDKClaudeAgentSDK,
			Class:    "AgentDefinition",
			Language: models.LanguageTypeScript,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(prop.StartPoint().Row) + 1,
				EndLine:  int(prop.EndPoint().Row) + 1,
			},
			Name:          name,
			MCPServerRefs: mcpRefs,
		}
		if valNode.Type() != "object" {
			agent.Opaque = true
		} else {
			agent.Kwargs = astutil.TSObjectKwargs(valNode, pf.Source)
		}
		populateTSAgentToolRefs(&agent)
		out = append(out, agent)
	}
	return out
}

// extractTSMCPServerRefs builds the MCPServerRef list from
// options.mcpServers. Object-literal values produce refs whose Class is
// the TS union-member name (McpStdioServerConfig etc.); identifier values
// produce refs whose Class is "createSdkMcpServer" (the only function that
// returns an MCP server in the TS SDK — same-file resolution to the actual
// MCPServerDef happens in ResolveEdges, out of scope for SP1).
func extractTSMCPServerRefs(options *sitter.Node, pf ParsedFile) []models.MCPServerRef {
	servers := getObjectProperty(options, "mcpServers", pf.Source)
	if servers == nil || servers.Type() != "object" {
		return nil
	}
	var refs []models.MCPServerRef
	for i := 0; i < int(servers.NamedChildCount()); i++ {
		prop := servers.NamedChild(i)
		if prop.Type() != "pair" {
			continue
		}
		val := prop.ChildByFieldName("value")
		if val == nil {
			continue
		}
		switch val.Type() {
		case "object":
			typeNode := getObjectProperty(val, "type", pf.Source)
			if typeNode == nil || typeNode.Type() != "string" {
				continue
			}
			raw := astutil.NodeText(typeNode, pf.Source)
			if len(raw) < 2 {
				continue
			}
			class := tsMCPClassForTransport(raw[1 : len(raw)-1])
			if class == "" {
				continue
			}
			refs = append(refs, models.MCPServerRef{Class: class, DefIndex: -1})
		case "identifier":
			refs = append(refs, models.MCPServerRef{Class: "createSdkMcpServer", DefIndex: -1})
		}
	}
	return refs
}

// getObjectProperty returns the value node of `obj.prop` if obj is an object
// literal with a literal property_identifier or string key matching `key`;
// nil otherwise.
func getObjectProperty(obj *sitter.Node, key string, src []byte) *sitter.Node {
	if obj == nil || obj.Type() != "object" {
		return nil
	}
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		prop := obj.NamedChild(i)
		if prop.Type() != "pair" {
			continue
		}
		k := prop.ChildByFieldName("key")
		v := prop.ChildByFieldName("value")
		if k == nil || v == nil {
			continue
		}
		var kname string
		switch k.Type() {
		case "property_identifier":
			kname = astutil.NodeText(k, src)
		case "string":
			raw := astutil.NodeText(k, src)
			if len(raw) >= 2 {
				kname = raw[1 : len(raw)-1]
			}
		}
		if kname == key {
			return v
		}
	}
	return nil
}

func extractTypedConstAgent(decl *sitter.Node, pf ParsedFile) (models.AgentDef, bool) {
	nameNode := decl.ChildByFieldName("name")
	typeNode := decl.ChildByFieldName("type")
	valueNode := decl.ChildByFieldName("value")
	if nameNode == nil || typeNode == nil || valueNode == nil {
		return models.AgentDef{}, false
	}
	if nameNode.Type() != "identifier" || valueNode.Type() != "object" {
		return models.AgentDef{}, false
	}
	// type field text looks like ": AgentDefinition" — substring check.
	if !strings.Contains(astutil.NodeText(typeNode, pf.Source), "AgentDefinition") {
		return models.AgentDef{}, false
	}
	name := astutil.NodeText(nameNode, pf.Source)
	agent := models.AgentDef{
		SDK:      models.SDKClaudeAgentSDK,
		Class:    "AgentDefinition",
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(decl.StartPoint().Row) + 1,
			EndLine:  int(decl.EndPoint().Row) + 1,
		},
		Name:    name,
		VarName: name,
		Kwargs:  astutil.TSObjectKwargs(valueNode, pf.Source),
	}
	populateTSAgentToolRefs(&agent)
	return agent, true
}

// populateTSAgentToolRefs reads agent.Kwargs.Children["tools"] (if it's a
// list of string literals) and appends one ToolRef per entry. Builtin tool
// names like "Read"/"Bash" stay as strings — they're not resolved to
// ToolDefs (which represent user-defined tools).
func populateTSAgentToolRefs(a *models.AgentDef) {
	if a.Kwargs == nil {
		return
	}
	tools := a.Kwargs.Children["tools"]
	if tools == nil || tools.Value == nil || tools.Value.Kind != models.ExprList {
		return
	}
	for _, item := range tools.Value.List {
		if item.Kind != models.ExprLiteralString {
			continue
		}
		raw := item.Text
		if len(raw) < 2 {
			continue
		}
		name := raw[1 : len(raw)-1]
		a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: name})
	}
}
