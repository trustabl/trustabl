package rules

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// ─── bool predicates ─────────────────────────────────────────────────────────

func PredHasDocstring(t models.ToolDef) bool {
	return strings.TrimSpace(t.Description) != ""
}

func PredHasParams(t models.ToolDef) bool {
	return len(t.ParamNames) > 0
}

func PredHasTypedParams(t models.ToolDef) bool {
	return t.HasTypedParams
}

func PredHasRaise(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	return len(astutil.FindAll(root, "raise_statement")) > 0
}

func PredHasTryExcept(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	return len(astutil.FindAll(root, "try_statement")) > 0
}

func PredHasShellCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		c := astutil.NodeText(fn, pf.Source)
		if strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" {
			found = true
			return false
		}
		return true
	})
	return found
}

func PredHasWriteCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		if callee == "open" {
			args := n.ChildByFieldName("arguments")
			if args != nil {
				text := astutil.NodeText(args, pf.Source)
				if strings.Contains(text, `"w"`) || strings.Contains(text, `'w'`) ||
					strings.Contains(text, `"a"`) || strings.Contains(text, `'a'`) ||
					strings.Contains(text, `"x"`) || strings.Contains(text, `'x'`) {
					found = true
					return false
				}
			}
			return true
		}
		if callee == "shutil.copy" || callee == "shutil.copy2" ||
			callee == "shutil.move" || callee == "shutil.rmtree" {
			found = true
			return false
		}
		return true
	})
	return found
}

func PredHasDynamicURLCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		if !analysis.IsHTTPCall(astutil.NodeText(fn, pf.Source)) {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		if int(args.NamedChildCount()) > 0 {
			first := args.NamedChild(0)
			if first.Type() != "string" {
				found = true
			} else {
				for i := 0; i < int(first.NamedChildCount()); i++ {
					if first.NamedChild(i).Type() == "interpolation" {
						found = true
						break
					}
				}
			}
		}
		return !found
	})
	return found
}

// ─── string-list predicates ───────────────────────────────────────────────────

func PredNameIn(names []string, t models.ToolDef) bool {
	lower := strings.ToLower(t.Name)
	for _, n := range names {
		if lower == strings.ToLower(n) {
			return true
		}
	}
	return false
}

func PredNameHasPrefix(prefixes []string, t models.ToolDef) bool {
	lower := strings.ToLower(t.Name)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func PredHasBodyText(needles []string, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	body := astutil.NodeText(root, pf.Source)
	for _, needle := range needles {
		if strings.Contains(body, needle) {
			return true
		}
	}
	return false
}

func PredParamNameMatches(expr ParamNameMatchExpr, t models.ToolDef) bool {
	for _, p := range t.ParamNames {
		lower := strings.ToLower(p)
		for _, e := range expr.Exact {
			if lower == strings.ToLower(e) {
				return true
			}
		}
		for _, c := range expr.Contains {
			if strings.Contains(lower, strings.ToLower(c)) {
				return true
			}
		}
		for _, s := range expr.Suffixes {
			if strings.HasSuffix(lower, strings.ToLower(s)) {
				return true
			}
		}
		for _, pr := range expr.Prefixes {
			if strings.HasPrefix(lower, strings.ToLower(pr)) {
				return true
			}
		}
	}
	return false
}

// ─── call-site predicates ─────────────────────────────────────────────────────

func PredCallWithoutKwarg(expr CallWithoutKwargExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		if _, ok := calleeSet[astutil.NodeText(fn, pf.Source)]; !ok {
			return true
		}
		if !astutil.HasKwarg(n, pf.Source, expr.Missing) {
			found = true
		}
		return !found
	})
	return found
}

func PredCallWithKwargValue(expr CallWithKwargValueExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		matches := false
		if _, ok := calleeSet[callee]; ok {
			matches = true
		}
		if !matches && expr.CalleePrefix != "" && strings.HasPrefix(callee, expr.CalleePrefix) {
			matches = true
		}
		if !matches {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		astutil.Walk(args, func(kn *sitter.Node) bool {
			if kn.Type() != "keyword_argument" {
				return true
			}
			kname := kn.ChildByFieldName("name")
			kval := kn.ChildByFieldName("value")
			if kname == nil || kval == nil {
				return true
			}
			if astutil.NodeText(kname, pf.Source) == expr.Kwarg &&
				astutil.NodeText(kval, pf.Source) == expr.Value {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}

// PredCallUsesUnnormalizedPathParam mirrors the per-param CSDK-004 detector:
// fires when a path-like parameter flows to an I/O call AND that specific
// param has not been normalized (.resolve()/realpath()) earlier in the
// function. Returns true on the first unsafe pairing.
func PredCallUsesUnnormalizedPathParam(expr CallUsesUnnormalizedPathParamExpr, t models.ToolDef, pf analysis.ParsedFile) bool {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	pathish := map[string]bool{}
	for _, p := range t.ParamNames {
		if analysis.IsPathishParam(p) {
			pathish[p] = true
		}
	}
	if len(pathish) == 0 {
		return false
	}
	normalized := normalizedPathParams(root, pf.Source, pathish)

	calleeSet := make(map[string]struct{}, len(expr.Callees))
	for _, c := range expr.Callees {
		calleeSet[c] = struct{}{}
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		callee := astutil.NodeText(fn, pf.Source)
		matches := false
		if _, ok := calleeSet[callee]; ok {
			matches = true
		}
		if !matches {
			for _, pref := range expr.CalleePrefixes {
				if strings.HasPrefix(callee, pref) {
					matches = true
					break
				}
			}
		}
		if !matches {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		astutil.Walk(args, func(arg *sitter.Node) bool {
			if found {
				return false
			}
			if arg.Type() != "identifier" {
				return true
			}
			name := astutil.NodeText(arg, pf.Source)
			if pathish[name] && !normalized[name] {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}

// normalizedPathParams returns the set of pathish identifier names that
// appear as the receiver of .resolve() or as the argument to realpath()
// inside fn. Mirrors the discovery logic in the original CSDK-004 detector.
func normalizedPathParams(fn *sitter.Node, src []byte, pathish map[string]bool) map[string]bool {
	out := map[string]bool{}
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fnNode := n.ChildByFieldName("function")
		if fnNode == nil {
			return true
		}
		callee := astutil.NodeText(fnNode, src)
		if callee == "realpath" || callee == "os.path.realpath" {
			if args := n.ChildByFieldName("arguments"); args != nil {
				count := int(args.NamedChildCount())
				for i := 0; i < count; i++ {
					c := args.NamedChild(i)
					if c.Type() == "identifier" {
						name := astutil.NodeText(c, src)
						if pathish[name] {
							out[name] = true
							break
						}
					}
				}
			}
			return true
		}
		if fnNode.Type() == "attribute" {
			attr := fnNode.ChildByFieldName("attribute")
			if attr != nil && astutil.NodeText(attr, src) == "resolve" {
				obj := fnNode.ChildByFieldName("object")
				if obj != nil {
					astutil.Walk(obj, func(m *sitter.Node) bool {
						if m.Type() != "identifier" {
							return true
						}
						name := astutil.NodeText(m, src)
						if pathish[name] {
							out[name] = true
						}
						return true
					})
				}
			}
		}
		return true
	})
	return out
}

// ─── tool decorator predicates ────────────────────────────────────────────────

func PredToolDecoratorKwargValue(expr ToolDecoratorKwargValueExpr, t models.ToolDef) bool {
	v, ok := t.Config[expr.Kwarg]
	return ok && v == expr.Value
}

func PredToolDecoratorKwargPresent(names []string, t models.ToolDef) bool {
	for _, n := range names {
		if _, ok := t.Config[n]; ok {
			return true
		}
	}
	return false
}

// ─── agent predicates ─────────────────────────────────────────────────────────

func PredAgentClass(classes []string, a models.AgentDef) bool {
	for _, c := range classes {
		if a.Class == c {
			return true
		}
	}
	return false
}

// lookupKwarg walks a dotted-path like "model_settings.tool_choice" through
// the KwargTree. Returns nil if any segment is missing.
func lookupKwarg(a models.AgentDef, path string) *models.KwargTree {
	if a.Kwargs == nil {
		return nil
	}
	parts := strings.Split(path, ".")
	cur := a.Kwargs
	for _, p := range parts {
		if cur.Children == nil {
			return nil
		}
		next, ok := cur.Children[p]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func PredAgentKwargPresent(paths []string, a models.AgentDef) bool {
	for _, p := range paths {
		if lookupKwarg(a, p) != nil {
			return true
		}
	}
	return false
}

func PredAgentKwargMissing(paths []string, a models.AgentDef) bool {
	for _, p := range paths {
		if lookupKwarg(a, p) == nil {
			return true
		}
	}
	return false
}

func PredAgentKwargListEmpty(paths []string, a models.AgentDef) bool {
	for _, p := range paths {
		kw := lookupKwarg(a, p)
		if kw == nil {
			return true // absent counts as empty
		}
		if kw.Value == nil {
			continue
		}
		if kw.Value.Kind == models.ExprList && len(kw.Value.List) == 0 {
			return true
		}
	}
	return false
}

func PredAgentKwargValue(expr AgentKwargValueExpr, a models.AgentDef) bool {
	kw := lookupKwarg(a, expr.Kwarg)
	if kw == nil || kw.Value == nil {
		return false
	}
	raw := kw.Value.Text
	if kw.Value.Kind == models.ExprLiteralString {
		raw = strings.Trim(raw, `"'`)
	}
	return raw == expr.Value
}

func PredAgentUsesToolKind(kinds []string, a models.AgentDef, inv models.RepoInventory) bool {
	for _, ref := range a.ToolRefs {
		if ref.Resolved == nil {
			continue
		}
		for _, k := range kinds {
			if string(ref.Resolved.Kind) == k {
				return true
			}
		}
	}
	return false
}

// PredAgentGrantsBuiltinTool fires when the agent's tools= list contains a
// built-in tool name (a string literal like "Bash"), as opposed to a
// reference to a discovered ToolDef. Used for Claude AgentDefinition, whose
// tools= holds built-in tool name strings that resolve to no ToolDef and so
// are invisible to agent_uses_tool_kind.
func PredAgentGrantsBuiltinTool(names []string, a models.AgentDef) bool {
	for _, ref := range a.ToolRefs {
		got := strings.Trim(ref.Name, `"'`)
		for _, want := range names {
			if got == want {
				return true
			}
		}
	}
	return false
}

func PredAgentHandoffToClass(classes []string, a models.AgentDef) bool {
	for _, ref := range a.HandoffRefs {
		if ref.Resolved == nil {
			continue
		}
		for _, c := range classes {
			if ref.Resolved.Class == c {
				return true
			}
		}
	}
	return false
}

// PredAgentUsesHostedToolClass fires when the agent's HostedToolRefs include
// any of the named classes. Matches by the Class string on the ref itself
// (does not require Resolved), so unresolved hosted-tool refs still count.
func PredAgentUsesHostedToolClass(classes []string, a models.AgentDef) bool {
	for _, ref := range a.HostedToolRefs {
		for _, want := range classes {
			if ref.Class == want {
				return true
			}
		}
	}
	return false
}

// PredAgentIsSubagentOfAny returns true if the agent under test appears as
// a Resolved target in any other agent's HandoffRefs in the inventory.
// Matching is by Name+FilePath (Resolved is a pointer to a specific def in
// inv.Agents, so identity comparison would also work but is fragile across
// re-slicing). Self-handoff edges count.
func PredAgentIsSubagentOfAny(a models.AgentDef, inv models.RepoInventory) bool {
	for _, other := range inv.Agents {
		for _, ref := range other.HandoffRefs {
			if ref.Resolved == nil {
				continue
			}
			if ref.Resolved.Name == a.Name && ref.Resolved.FilePath == a.FilePath {
				return true
			}
		}
	}
	return false
}

// ─── repo predicates ──────────────────────────────────────────────────────────

func PredRepoHasSDKDep(names []string, p models.RepoProfile) bool {
	for _, dep := range p.SDKDeps {
		for _, n := range names {
			if dep.Name == n {
				return true
			}
		}
	}
	return false
}

func PredRepoHasSDKInCode(sdks []string, inv models.RepoInventory) bool {
	for _, s := range inv.SDKsDetected {
		for _, want := range sdks {
			if string(s) == want {
				return true
			}
		}
	}
	return false
}

func PredRepoHasAgentClass(classes []string, inv models.RepoInventory) bool {
	for _, a := range inv.Agents {
		for _, c := range classes {
			if a.Class == c {
				return true
			}
		}
	}
	return false
}

func PredRepoHasNoAgentClass(classes []string, inv models.RepoInventory) bool {
	return !PredRepoHasAgentClass(classes, inv)
}

func PredRepoComponentPresent(kinds []string, p models.RepoProfile) bool {
	for _, c := range p.Manifest.Components {
		for _, k := range kinds {
			if string(c.Kind) == k {
				return true
			}
		}
	}
	return false
}

// PredRepoUsesDefaultTracing checks whether the repo uses default OpenAI
// tracing (no custom add_trace_processor configured). The scanner computes
// UsesDefaultTracing from parsed source and stores it on the inventory.
func PredRepoUsesDefaultTracing(want bool, inv models.RepoInventory) bool {
	return inv.UsesDefaultTracing == want
}
