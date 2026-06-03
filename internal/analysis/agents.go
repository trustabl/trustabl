package analysis

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverAgents walks each ParsedFile and returns AgentDef records for every
// Agent(...) / SandboxAgent(...) / AgentDefinition(...) constructor call.
func DiscoverAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		out = append(out, discoverAgentsInFile(pf)...)
	}
	return out
}

// DiscoverGuardrails finds @input_guardrail and @output_guardrail decorated
// functions. Class-based guardrails are NOT detected in v1 (documented limitation).
func DiscoverGuardrails(files []ParsedFile) []models.GuardrailDef {
	var out []models.GuardrailDef
	for _, pf := range files {
		out = append(out, discoverGuardrailsInFile(pf)...)
	}
	return out
}

// DiscoverSessions finds construction sites for *Session classes from the
// agents SDK (SQLiteSession, EncryptedSession, RedisSession, etc.).
func DiscoverSessions(files []ParsedFile) []models.SessionUse {
	var out []models.SessionUse
	for _, pf := range files {
		out = append(out, discoverSessionsInFile(pf)...)
	}
	return out
}

// ResolveEdges resolves the symbol references inside each AgentDef.ToolRefs /
// HandoffRefs / InputGuards / OutputGuards against the inventory. Sets
// Resolved when the symbol is found, External=true otherwise.
func ResolveEdges(inv *models.RepoInventory, parsed []ParsedFile) {
	// Sort a COPY: ResolveEdges only needs deterministic internal iteration
	// order, but the caller passes a slice (e.g. append(parsed, tsFiles...)) it
	// does not expect to be reordered. Mutating a caller-owned argument as a
	// hidden side effect is a footgun the signature doesn't advertise.
	parsed = append([]ParsedFile(nil), parsed...)
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].RelPath < parsed[j].RelPath })

	toolsByFileSym := make(map[string]map[string]*models.ToolDef)
	for i := range inv.Tools {
		t := &inv.Tools[i]
		if toolsByFileSym[t.FilePath] == nil {
			toolsByFileSym[t.FilePath] = make(map[string]*models.ToolDef)
		}
		toolsByFileSym[t.FilePath][t.Name] = t
		// TS tools register a distinct const-binding name (VarName); also
		// index by it so `tools: [computeSum]` resolves when the registered
		// tool name is "sum". For Python tools VarName is empty — no-op.
		if t.VarName != "" && t.VarName != t.Name {
			toolsByFileSym[t.FilePath][t.VarName] = t
		}
	}
	guardsByFileSym := make(map[string]map[string]*models.GuardrailDef)
	for i := range inv.Guardrails {
		g := &inv.Guardrails[i]
		if guardsByFileSym[g.FilePath] == nil {
			guardsByFileSym[g.FilePath] = make(map[string]*models.GuardrailDef)
		}
		guardsByFileSym[g.FilePath][g.Name] = g
		// TS guardrails register a distinct const-binding name (VarName); also
		// index by it so `inputGuardrails: [blockPII]` resolves when the
		// registered guardrail name is "block_pii". For Python guardrails
		// VarName is empty — no-op.
		if g.VarName != "" && g.VarName != g.Name {
			guardsByFileSym[g.FilePath][g.VarName] = g
		}
	}
	// mcpByFileSym indexes pre-existing MCPServerDefs by (file, VarName) and
	// stores the ORIGINAL slice index (not a pointer). Storing the index
	// instead of a pointer is deliberate: the Python mcp_servers= block below
	// appends to inv.MCPServers inside the per-agent loop, which may
	// reallocate the backing array and invalidate any cached pointers. The
	// index stays valid because the original entries are never moved or
	// removed, only new entries are appended after them.
	mcpByFileSym := make(map[string]map[string]int)
	for i := range inv.MCPServers {
		m := &inv.MCPServers[i]
		// Python MCPServerDefs typically have VarName empty (the with-statement
		// alias path uses mcpAliasesByFile, not this index). TS ones carry
		// VarName from `const x = new MCPServerStdio(...)` — index by it so
		// `mcpServers: [x]` from a TS agent resolves to the same-file def.
		if m.VarName == "" {
			continue
		}
		if mcpByFileSym[m.FilePath] == nil {
			mcpByFileSym[m.FilePath] = make(map[string]int)
		}
		mcpByFileSym[m.FilePath][m.VarName] = i
	}

	importsByFile := buildImportsByFile(parsed)

	mcpAliasesByFile := make(map[string]map[string]models.MCPServerDef)
	for _, pf := range parsed {
		mcpAliasesByFile[pf.RelPath] = collectWithStatementMCPAliases(pf)
	}

	for i := range inv.Agents {
		a := &inv.Agents[i]
		// Opaque agents skip the Python kwarg blocks below (Kwargs can't be
		// trusted on Agent(**config)). But TS opaque-spread agents like
		// `new Agent({...defaults, tools: [webSearchTool()]})` still populate
		// HostedToolRefs/MCPServerRefs/ToolRefs at discovery from explicit
		// syntactic positions before the spread — those refs ARE trustworthy
		// and must still flow through the language-agnostic resolution passes
		// further down. The Python kwarg blocks no-op for TS anyway (TS uses
		// camelCase keys; the tools= block is already TS-gated), so skipping
		// them is safe even for non-opaque TS agents.
		if a.Opaque && a.Language != models.LanguageTypeScript {
			continue
		}

		// Python-shape tools= kwarg processing. TS OpenAI agents have their
		// ToolRefs and HostedToolRefs pre-populated by populateTSOpenAIToolRefs
		// at discovery (the call_expression items in Kwargs.tools would otherwise
		// fall through to External ToolRef emission here, double-emitting).
		if a.Language != models.LanguageTypeScript {
			toolsKwarg := agentKwarg(a, "tools")
			if toolsKwarg != nil && toolsKwarg.Value != nil && toolsKwarg.Value.Kind == models.ExprList {
				for _, item := range toolsKwarg.Value.List {
					// Hosted-tool call (e.g. WebSearchTool(), BashTool()) — emit a
					// HostedToolDef and a HostedToolRef. These never resolve to a
					// ToolDef. Classification is dispatched by the agent's SDK: each
					// SDK has its own closed class list (HostedToolClasses for OpenAI,
					// ADKHostedToolClasses for Google ADK), consulted only against its
					// own agents.
					var (
						h    models.HostedToolDef
						isHT bool
					)
					switch a.SDK {
					case models.SDKGoogleADK:
						h, isHT = classifyADKHostedToolCall(item, a.FilePath)
					default:
						h, isHT = classifyHostedToolCall(item, a.FilePath)
					}
					if isHT {
						inv.HostedTools = append(inv.HostedTools, h)
						a.HostedToolRefs = append(a.HostedToolRefs, models.HostedToolRef{
							Class:    h.Class,
							DefIndex: len(inv.HostedTools) - 1,
						})
						continue
					}
					// ADK wraps user functions as FunctionTool(symbol); the
					// registered ToolDef is keyed by the inner symbol, so unwrap
					// before symbol resolution.
					lookupName := item.Text
					if a.SDK == models.SDKGoogleADK {
						if inner, ok := adkFunctionToolArg(item.Text); ok {
							lookupName = inner
						}
					}
					ref := models.ToolRef{Name: lookupName}
					var td *models.ToolDef
					if t := toolsByFileSym[a.FilePath][lookupName]; t != nil {
						td = t
					} else if imp, ok := importsByFile[a.FilePath][lookupName]; ok {
						for _, candidateFile := range parsed {
							if matchesModule(candidateFile.RelPath, imp.module) {
								if cand := toolsByFileSym[candidateFile.RelPath][imp.name]; cand != nil {
									td = cand
									break
								}
							}
						}
					}
					if td != nil {
						ref.Resolved = td
					} else {
						ref.External = true
					}
					a.ToolRefs = append(a.ToolRefs, ref)
				}
			} else if toolsKwarg != nil {
				a.Opaque = true
			}
		}

		mcpKwarg := agentKwarg(a, "mcp_servers")
		if mcpKwarg != nil && mcpKwarg.Value != nil && mcpKwarg.Value.Kind == models.ExprList {
			for _, item := range mcpKwarg.Value.List {
				if m, ok := classifyMCPServerCall(item, a.FilePath); ok {
					inv.MCPServers = append(inv.MCPServers, m)
					a.MCPServerRefs = append(a.MCPServerRefs, models.MCPServerRef{
						Class:    m.Class,
						DefIndex: len(inv.MCPServers) - 1,
					})
					continue
				}
				// Alias from `async with MCPServer*(...) as srv:`. The def now
				// carries the MCP server's own definition line (the with-statement
				// call line + end), taken from aliasDef.Location. Each ref records
				// its def's pre-sort index (DefIndex); the post-sort remap re-points
				// .Resolved via the sort permutation, so refs resolve to distinct
				// defs even when N agents share one alias. v1 simplification: one
				// alias referenced by N agents yields N MCPServerDef entries, one per agent.
				// TODO(v2): alias resolution is same-file only — an alias imported from
				// another module is not resolved and falls through to External.
				if item.Kind == models.ExprNameRef {
					if aliasDef, ok := mcpAliasesByFile[a.FilePath][item.Text]; ok {
						inv.MCPServers = append(inv.MCPServers, models.MCPServerDef{
							Class:     aliasDef.Class,
							Transport: aliasDef.Transport,
							SDK:       models.SDKOpenAIAgents,
							Language:  models.LanguagePython,
							Location:  aliasDef.Location,
						})
						a.MCPServerRefs = append(a.MCPServerRefs, models.MCPServerRef{
							Class:    aliasDef.Class,
							DefIndex: len(inv.MCPServers) - 1,
						})
						continue
					}
				}
				a.MCPServerRefs = append(a.MCPServerRefs, models.MCPServerRef{
					Class:    item.Text,
					External: true,
					DefIndex: -1,
				})
			}
		} else if mcpKwarg != nil {
			// Intentional asymmetry vs. tools=: a non-list mcp_servers= value
			// (e.g. mcp_servers=server_list_var) does NOT set a.Opaque, because
			// MCP-server opaqueness is orthogonal to tool-list opaqueness for
			// downstream rules. A future rule that needs an "MCP servers were
			// declared but their identities are opaque" signal would set a
			// dedicated flag on AgentDef, not reuse Opaque.
		}

		// sub_agents= (ADK delegation tree). Resolves same-file agent name refs
		// into HandoffRefs for predicate uniformity with OpenAI's handoffs=.
		// Python-only: TS ADK discovery pre-populates HandoffRefs from
		// camelCase subAgents at parse time (ts_adk_agents.go), and the
		// language-agnostic resolve pass below wires them — re-walking the
		// kwarg here for TS would double-emit.
		if a.SDK == models.SDKGoogleADK && a.Language != models.LanguageTypeScript {
			subKwarg := agentKwarg(a, "sub_agents")
			if subKwarg != nil && subKwarg.Value != nil && subKwarg.Value.Kind == models.ExprList {
				agentsByName := map[string]*models.AgentDef{}
				for j := range inv.Agents {
					if inv.Agents[j].FilePath != a.FilePath {
						continue
					}
					// Key by both the name= literal and the assignment-target
					// variable, because sub_agents=[X] references the variable
					// while findings attribute to the name= value.
					if n := inv.Agents[j].Name; n != "" {
						agentsByName[n] = &inv.Agents[j]
					}
					if v := inv.Agents[j].VarName; v != "" {
						agentsByName[v] = &inv.Agents[j]
					}
				}
				for _, item := range subKwarg.Value.List {
					ref := models.AgentRef{Name: item.Text}
					if target, ok := agentsByName[item.Text]; ok {
						ref.Resolved = target
					} else {
						ref.External = true
					}
					a.HandoffRefs = append(a.HandoffRefs, ref)
				}
			}
		}

		// handoffs= (OpenAI Agents SDK delegation). Captured in Kwargs but,
		// unlike ADK's sub_agents=, was never turned into HandoffRefs — which
		// left PredAgentIsSubagentOfAny blind to every Python OpenAI handoff
		// edge. Append the referenced names as unresolved HandoffRefs; the
		// language-agnostic resolve pass below (gated on len(HandoffRefs) > 0)
		// wires them to same-file AgentDefs by Name or VarName. TS OpenAI
		// handoffs are pre-populated at discovery (ts_openai_agents.go), so this
		// kwarg walk is Python-only to avoid double-emitting. v1 limitation: a
		// list item wrapped in the handoff(...) helper resolves to External
		// rather than the target (bare agent refs are the common case).
		if a.SDK == models.SDKOpenAIAgents && a.Language != models.LanguageTypeScript {
			if hk := agentKwarg(a, "handoffs"); hk != nil && hk.Value != nil && hk.Value.Kind == models.ExprList {
				for _, item := range hk.Value.List {
					a.HandoffRefs = append(a.HandoffRefs, models.AgentRef{Name: item.Text})
				}
			}
		}

		resolveGuardKwarg(a, "input_guardrails", &a.InputGuards, guardsByFileSym[a.FilePath])
		resolveGuardKwarg(a, "output_guardrails", &a.OutputGuards, guardsByFileSym[a.FilePath])

		// Resolve pre-populated refs by same-file (VarName or Name) lookup.
		// TS OpenAI discovery pre-populates ToolRefs, MCPServerRefs,
		// InputGuards, OutputGuards, and HostedToolRefs at discovery time —
		// the Python loop bodies above are keyed off snake_case Kwargs
		// (tools/mcp_servers/input_guardrails/output_guardrails) and the TS
		// Kwargs use camelCase, so they no-op for TS agents. These passes
		// resolve those pre-populated refs.
		//
		// The Tool/Guardrail/MCP passes are intentionally language-agnostic:
		// they only act on refs with Resolved==nil && External==false (or
		// MCP DefIndex<0 && !External), which the Python blocks above never
		// leave behind (they always set one or the other). So real Python
		// flows are unaffected — only direct test setups or future SDKs
		// that pre-populate refs hit these passes.
		toolLookup := toolsByFileSym[a.FilePath]
		for j := range a.ToolRefs {
			ref := &a.ToolRefs[j]
			if ref.Resolved != nil || ref.External {
				continue
			}
			if td, ok := toolLookup[ref.Name]; ok {
				ref.Resolved = td
			} else {
				ref.External = true
			}
		}

		// HandoffRefs — TS ADK discovery pre-populates these from camelCase
		// subAgents= at parse time. Resolve same-file refs by Name or VarName;
		// the Python sub_agents= block above does both append and resolve in
		// one pass (and never leaves Resolved==nil && External==false), so
		// this language-agnostic pass only fires for pre-populated refs.
		if len(a.HandoffRefs) > 0 {
			agentsByName := map[string]*models.AgentDef{}
			for j := range inv.Agents {
				if inv.Agents[j].FilePath != a.FilePath {
					continue
				}
				if n := inv.Agents[j].Name; n != "" {
					agentsByName[n] = &inv.Agents[j]
				}
				if v := inv.Agents[j].VarName; v != "" {
					agentsByName[v] = &inv.Agents[j]
				}
			}
			for j := range a.HandoffRefs {
				ref := &a.HandoffRefs[j]
				if ref.Resolved != nil || ref.External {
					continue
				}
				if target, ok := agentsByName[ref.Name]; ok {
					ref.Resolved = target
				} else {
					ref.External = true
				}
			}
		}

		guardLookup := guardsByFileSym[a.FilePath]
		resolveGuardRefs := func(refs []models.GuardrailRef) {
			for j := range refs {
				ref := &refs[j]
				if ref.Resolved != nil || ref.External {
					continue
				}
				if gd, ok := guardLookup[ref.Name]; ok {
					ref.Resolved = gd
				} else {
					ref.External = true
				}
			}
		}
		resolveGuardRefs(a.InputGuards)
		resolveGuardRefs(a.OutputGuards)

		// MCPServerRefs — discovery sets Class=identifier text and
		// DefIndex=-1. Resolve via same-file VarName lookup; on success
		// replace Class with the canonical MCP class name and set
		// DefIndex to the original slice index. The post-sort remap
		// below re-points .Resolved via the sort permutation.
		mcpLookup := mcpByFileSym[a.FilePath]
		for j := range a.MCPServerRefs {
			ref := &a.MCPServerRefs[j]
			if ref.DefIndex >= 0 || ref.External {
				continue
			}
			if idx, ok := mcpLookup[ref.Class]; ok {
				ref.Class = inv.MCPServers[idx].Class
				ref.DefIndex = idx
			} else {
				ref.External = true
			}
		}

		// HostedToolRefs — TS discovery (OpenAI and ADK) sets Class to the
		// canonical hosted-tool class name and DefIndex=-1. Materialize a
		// matching HostedToolDef in inv.HostedTools so downstream consumers
		// see a complete hosted-tool inventory. The SDK is stamped from
		// whichever closed set matched the class name — TSOpenAIHostedToolFactories
		// → SDKOpenAIAgents, TSADKHostedToolClasses → SDKGoogleADK. Python
		// hosted-tool refs (handled by the classify block above) carry a
		// valid DefIndex already and are skipped by the first guard. The
		// Location is approximated to the agent's call site — the precise
		// factory-call line is not currently carried on HostedToolRef.
		// DefIndex set here is the pre-sort index; the post-sort remap
		// below re-points .Resolved via the sort permutation.
		for j := range a.HostedToolRefs {
			ref := &a.HostedToolRefs[j]
			if ref.DefIndex >= 0 {
				continue
			}
			// Recognize either TS OpenAI factories or TS ADK hosted classes.
			// SDK is stamped from whichever set matched so the inventory
			// attributes the def to the correct SDK.
			var sdk models.SDK
			switch {
			case IsTSOpenAIHostedToolFactory(ref.Class):
				sdk = models.SDKOpenAIAgents
			case IsTSADKHostedToolClass(ref.Class):
				sdk = models.SDKGoogleADK
			default:
				continue
			}
			inv.HostedTools = append(inv.HostedTools, models.HostedToolDef{
				Class: ref.Class,
				SDK:   sdk,
				Location: models.Location{
					FilePath: a.FilePath,
					Line:     a.Line,
				},
			})
			ref.DefIndex = len(inv.HostedTools) - 1
		}
	}

	hostedRemap := sortHostedTools(inv.HostedTools)
	mcpRemap := sortMCPServers(inv.MCPServers)

	// Re-point ref.Resolved after sorting. Each ref recorded the pre-sort index
	// of its def at append time (DefIndex); the sort permutation maps it to the
	// post-sort slot. DefIndex < 0 means the ref is external or could not be
	// resolved — left unresolved. This index-based remap is unambiguous even
	// when two agents in one file share a class or an MCP alias (the
	// content-matching it replaces was not).
	for i := range inv.Agents {
		a := &inv.Agents[i]
		for j := range a.HostedToolRefs {
			ref := &a.HostedToolRefs[j]
			if ref.DefIndex < 0 || ref.DefIndex >= len(hostedRemap) {
				continue
			}
			ref.Resolved = &inv.HostedTools[hostedRemap[ref.DefIndex]]
		}
		for j := range a.MCPServerRefs {
			ref := &a.MCPServerRefs[j]
			if ref.External || ref.DefIndex < 0 || ref.DefIndex >= len(mcpRemap) {
				continue
			}
			ref.Resolved = &inv.MCPServers[mcpRemap[ref.DefIndex]]
		}
	}
}

// ─── Internal helpers ─────────────────────────────────────────────────────

type agentImport struct {
	SDK   models.SDK
	Class string
}

func collectAgentImports(pf ParsedFile) map[string]agentImport {
	out := make(map[string]agentImport)
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "import_from_statement" {
			return true
		}
		moduleName := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
		var sdk models.SDK
		switch moduleName {
		case "agents":
			sdk = models.SDKOpenAIAgents
		case "claude_agent_sdk":
			sdk = models.SDKClaudeAgentSDK
		default:
			return true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child.Type() == "dotted_name" || child.Type() == "aliased_import" {
				name := astutil.NodeText(child, pf.Source)
				switch name {
				case "Agent", "SandboxAgent", "AgentDefinition":
					out[name] = agentImport{SDK: sdk, Class: name}
				}
			}
		}
		return true
	})
	return out
}

func discoverAgentsInFile(pf ParsedFile) []models.AgentDef {
	imports := collectAgentImports(pf)
	if len(imports) == 0 {
		return nil
	}

	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		funcName := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		imp, ok := imports[funcName]
		if !ok {
			return true
		}

		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      imp.SDK,
			Class:    imp.Class,
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			Kwargs: kwargs,
			Opaque: opaque,
		}
		if kwargs != nil && kwargs.Children["name"] != nil &&
			kwargs.Children["name"].Value != nil &&
			kwargs.Children["name"].Value.Kind == models.ExprLiteralString {
			a.Name = strings.Trim(kwargs.Children["name"].Value.Text, `"'`)
		}
		// Capture the assignment-target identifier (e.g. `multiply_agent =
		// Agent(...)`) so handoffs=[multiply_agent] references resolve by
		// variable name even when the Python variable differs from the name=
		// literal. Mirrors the ADK discovery (adk_agents.go); previously only
		// ADK and TS agents recorded VarName, so Python OpenAI handoff edges
		// could never resolve to their same-file target.
		if p := n.Parent(); p != nil && p.Type() == "assignment" {
			if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
				a.VarName = astutil.NodeText(l, pf.Source)
			}
		}
		// Claude's AgentDefinition has no name= kwarg — the agent is named by
		// its enclosing dict key (agents={"researcher": AgentDefinition(...)})
		// or by the assignment target (researcher = AgentDefinition(...)).
		if a.Name == "" {
			if p := n.Parent(); p != nil {
				switch p.Type() {
				case "pair":
					if k := p.ChildByFieldName("key"); k != nil && k.Type() == "string" {
						a.Name = strings.Trim(astutil.NodeText(k, pf.Source), `"'`)
					}
				case "assignment":
					if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
						a.Name = astutil.NodeText(l, pf.Source)
					}
				}
			}
		}
		out = append(out, a)
		return true
	})
	return out
}

// extractCallKwargs walks the argument_list of a call node and builds a
// KwargTree. Returns opaque=true if the call uses **unpack (e.g. Agent(**config)).
func extractCallKwargs(callNode *sitter.Node, src []byte) (*models.KwargTree, bool) {
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return nil, false
	}
	tree := &models.KwargTree{Children: map[string]*models.KwargTree{}}
	opaque := false
	for i := 0; i < int(args.ChildCount()); i++ {
		child := args.Child(i)
		switch child.Type() {
		case "keyword_argument":
			name := astutil.NodeText(child.ChildByFieldName("name"), src)
			value := child.ChildByFieldName("value")
			tree.Children[name] = exprFromNode(value, src)
		case "dictionary_splat":
			opaque = true
		}
	}
	if len(tree.Children) == 0 && !opaque {
		return nil, false
	}
	return tree, opaque
}

// exprFromNode converts a value AST node into a typed KwargTree leaf.
func exprFromNode(n *sitter.Node, src []byte) *models.KwargTree {
	if n == nil {
		return nil
	}
	e := &models.Expr{Text: astutil.NodeText(n, src)}
	e.Line = int(n.StartPoint().Row) + 1
	e.EndLine = int(n.EndPoint().Row) + 1
	switch n.Type() {
	case "string":
		e.Kind = models.ExprLiteralString
	case "integer":
		e.Kind = models.ExprLiteralInt
	case "float":
		e.Kind = models.ExprLiteralFloat
	case "true", "false":
		e.Kind = models.ExprLiteralBool
	case "none":
		e.Kind = models.ExprLiteralNone
	case "identifier":
		e.Kind = models.ExprNameRef
	case "list":
		e.Kind = models.ExprList
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			childTree := exprFromNode(child, src)
			if childTree != nil && childTree.Value != nil {
				e.List = append(e.List, *childTree.Value)
			}
		}
	case "call":
		e.Kind = models.ExprCall
	case "attribute":
		e.Kind = models.ExprNameRef
	default:
		e.Kind = models.ExprUnknown
	}
	// For ModelSettings(tool_choice="required") as a kwarg value, descend into
	// the call's kwargs so dotted-path lookups can find model_settings.tool_choice.
	if n.Type() == "call" {
		inner, _ := extractCallKwargs(n, src)
		children := nilToEmpty(inner).Children
		// Also carry the kwargs on the Expr itself, so list elements (which keep
		// only the Expr, not this KwargTree) retain a hosted-tool call's kwargs.
		e.CallKwargs = children
		return &models.KwargTree{Value: e, Children: children}
	}
	return &models.KwargTree{Value: e}
}

func nilToEmpty(t *models.KwargTree) *models.KwargTree {
	if t == nil {
		return &models.KwargTree{Children: map[string]*models.KwargTree{}}
	}
	return t
}

func discoverGuardrailsInFile(pf ParsedFile) []models.GuardrailDef {
	var out []models.GuardrailDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "decorated_definition" {
			return true
		}
		decoratorText := decoratorBlockText(n, pf.Source)
		var kind models.GuardrailKind
		switch {
		case strings.Contains(decoratorText, "@input_guardrail"):
			kind = models.GuardrailInput
		case strings.Contains(decoratorText, "@output_guardrail"):
			kind = models.GuardrailOutput
		default:
			return true
		}
		def := n.ChildByFieldName("definition")
		if def == nil {
			return true
		}
		name := astutil.FunctionName(def, pf.Source)
		out = append(out, models.GuardrailDef{
			Name:     name,
			Kind:     kind,
			Location: models.Location{FilePath: pf.RelPath, Line: int(def.StartPoint().Row) + 1, EndLine: int(def.EndPoint().Row) + 1},
		})
		return true
	})
	return out
}

func decoratorBlockText(decoratedDef *sitter.Node, src []byte) string {
	var b strings.Builder
	for i := 0; i < int(decoratedDef.ChildCount()); i++ {
		c := decoratedDef.Child(i)
		if c.Type() == "decorator" {
			b.WriteString(astutil.NodeText(c, src))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

var sessionClasses = map[string]bool{
	"SQLiteSession":         true,
	"SQLAlchemySession":     true,
	"RedisSession":          true,
	"MongoDBSession":        true,
	"EncryptedSession":      true,
	"AdvancedSQLiteSession": true,
}

func discoverSessionsInFile(pf ParsedFile) []models.SessionUse {
	imported := make(map[string]bool)
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "import_from_statement" {
			return true
		}
		moduleName := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
		if !strings.HasPrefix(moduleName, "agents") {
			return true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child.Type() == "dotted_name" {
				name := astutil.NodeText(child, pf.Source)
				if sessionClasses[name] {
					imported[name] = true
				}
			}
		}
		return true
	})
	if len(imported) == 0 {
		return nil
	}

	var out []models.SessionUse
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		funcName := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		if imported[funcName] {
			out = append(out, models.SessionUse{
				Class:    funcName,
				Location: models.Location{FilePath: pf.RelPath, Line: int(n.StartPoint().Row) + 1, EndLine: int(n.EndPoint().Row) + 1},
			})
		}
		return true
	})
	return out
}

func agentKwarg(a *models.AgentDef, name string) *models.KwargTree {
	if a.Kwargs == nil {
		return nil
	}
	return a.Kwargs.Children[name]
}

func resolveGuardKwarg(a *models.AgentDef, kwargName string, into *[]models.GuardrailRef, lookup map[string]*models.GuardrailDef) {
	kw := agentKwarg(a, kwargName)
	if kw == nil || kw.Value == nil || kw.Value.Kind != models.ExprList {
		return
	}
	for _, item := range kw.Value.List {
		ref := models.GuardrailRef{Name: item.Text}
		if g := lookup[item.Text]; g != nil {
			ref.Resolved = g
		} else {
			ref.External = true
		}
		*into = append(*into, ref)
	}
}

type importBinding struct {
	module string
	name   string
}

func buildImportsByFile(parsed []ParsedFile) map[string]map[string]importBinding {
	out := make(map[string]map[string]importBinding)
	for _, pf := range parsed {
		m := make(map[string]importBinding)
		astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
			if n.Type() != "import_from_statement" {
				return true
			}
			module := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type() == "dotted_name" {
					name := astutil.NodeText(child, pf.Source)
					if name != module {
						m[name] = importBinding{module: module, name: name}
					}
				} else if child.Type() == "aliased_import" {
					orig := astutil.NodeText(child.ChildByFieldName("name"), pf.Source)
					alias := astutil.NodeText(child.ChildByFieldName("alias"), pf.Source)
					m[alias] = importBinding{module: module, name: orig}
				}
			}
			return true
		})
		out[pf.RelPath] = m
	}
	return out
}

// matchesModule returns true if filePath corresponds to the dotted module name
// (e.g. "tools.py" matches "tools", "pkg/sub/m.py" matches "pkg.sub.m").
func matchesModule(filePath, module string) bool {
	base := strings.TrimSuffix(filePath, ".py")
	base = strings.ReplaceAll(base, "/", ".")
	return base == module || strings.HasSuffix(base, "."+module)
}
