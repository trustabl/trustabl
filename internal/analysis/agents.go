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
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].RelPath < parsed[j].RelPath })

	toolsByFileSym := make(map[string]map[string]*models.ToolDef)
	for i := range inv.Tools {
		t := &inv.Tools[i]
		if toolsByFileSym[t.FilePath] == nil {
			toolsByFileSym[t.FilePath] = make(map[string]*models.ToolDef)
		}
		toolsByFileSym[t.FilePath][t.Name] = t
	}
	guardsByFileSym := make(map[string]map[string]*models.GuardrailDef)
	for i := range inv.Guardrails {
		g := &inv.Guardrails[i]
		if guardsByFileSym[g.FilePath] == nil {
			guardsByFileSym[g.FilePath] = make(map[string]*models.GuardrailDef)
		}
		guardsByFileSym[g.FilePath][g.Name] = g
	}

	importsByFile := buildImportsByFile(parsed)

	for i := range inv.Agents {
		a := &inv.Agents[i]
		if a.Opaque {
			continue
		}

		toolsKwarg := agentKwarg(a, "tools")
		if toolsKwarg != nil && toolsKwarg.Value != nil && toolsKwarg.Value.Kind == models.ExprList {
			for _, item := range toolsKwarg.Value.List {
				// Hosted-tool call (e.g. WebSearchTool()) — emit a HostedToolDef
				// and a HostedToolRef. These never resolve to a ToolDef.
				if h, ok := classifyHostedToolCall(item, a.FilePath, a.Line); ok {
					inv.HostedTools = append(inv.HostedTools, h)
					ref := models.HostedToolRef{Class: h.Class}
					ref.Resolved = &inv.HostedTools[len(inv.HostedTools)-1]
					a.HostedToolRefs = append(a.HostedToolRefs, ref)
					continue
				}
				ref := models.ToolRef{Name: item.Text}
				var td *models.ToolDef
				if t := toolsByFileSym[a.FilePath][item.Text]; t != nil {
					td = t
				} else if imp, ok := importsByFile[a.FilePath][item.Text]; ok {
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

		resolveGuardKwarg(a, "input_guardrails", &a.InputGuards, guardsByFileSym[a.FilePath])
		resolveGuardKwarg(a, "output_guardrails", &a.OutputGuards, guardsByFileSym[a.FilePath])
	}

	sortHostedTools(inv.HostedTools)

	// Re-resolve HostedToolRef pointers after sorting. The append-and-take-address
	// pattern in the loop above leaves stale pointers when sort moves elements;
	// also, append itself can realloc the backing array. For each agent, walk
	// inv.HostedTools to find matches by (FilePath, Line, Class), consuming each
	// match at most once so duplicate classes in the same agent (e.g.
	// tools=[WebSearchTool(), WebSearchTool()]) resolve to distinct entries.
	for i := range inv.Agents {
		a := &inv.Agents[i]
		if len(a.HostedToolRefs) == 0 {
			continue
		}
		consumed := make(map[int]bool, len(a.HostedToolRefs))
		for j := range a.HostedToolRefs {
			ref := &a.HostedToolRefs[j]
			for k := range inv.HostedTools {
				if consumed[k] {
					continue
				}
				h := &inv.HostedTools[k]
				if h.FilePath == a.FilePath && h.Line == a.Line && h.Class == ref.Class {
					ref.Resolved = h
					consumed[k] = true
					break
				}
			}
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
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
			Kwargs:   kwargs,
			Opaque:   opaque,
		}
		if kwargs != nil && kwargs.Children["name"] != nil &&
			kwargs.Children["name"].Value != nil &&
			kwargs.Children["name"].Value.Kind == models.ExprLiteralString {
			a.Name = strings.Trim(kwargs.Children["name"].Value.Text, `"'`)
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
	switch n.Type() {
	case "string":
		e.Kind = models.ExprLiteralString
	case "integer":
		e.Kind = models.ExprLiteralInt
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
		return &models.KwargTree{Value: e, Children: nilToEmpty(inner).Children}
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
			FilePath: pf.RelPath,
			Line:     int(def.StartPoint().Row) + 1,
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
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
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
