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
	// TypeScript tools carry the signal as a discovery-computed fact (set by
	// tsHandlerFacts for child_process / exec / spawn callees); the Python AST
	// walk below does not understand the TS grammar. Branch on language so the
	// Python path stays byte-identical.
	if t.Language == models.LanguageTypeScript {
		return t.Facts["shells_out"] == "true"
	}
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
		if strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" ||
			strings.HasPrefix(c, "os.spawn") {
			found = true
			return false
		}
		return true
	})
	return found
}

// PredHasCodeExecCall reports whether the tool body invokes dynamic code
// execution primitives. For TypeScript tools it reads the discovery-computed
// "code_exec" fact (set by tsHandlerFacts for bare `eval(...)` callees and
// `new Function(...)` expressions). For Python tools it walks the AST for
// bare eval/exec/compile builtin callees — safe attribute calls like
// re.compile are not flagged, the false positive that substring matching on
// "compile(" cannot avoid.
func PredHasCodeExecCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	// TypeScript tools carry the signal as a discovery-computed fact; the
	// Python AST walk below does not understand the TS grammar. Branch on
	// language so the Python path stays byte-identical.
	if t.Language == models.LanguageTypeScript {
		return t.Facts["code_exec"] == "true"
	}
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
		switch astutil.NodeText(fn, pf.Source) {
		case "eval", "exec", "compile":
			found = true
			return false
		}
		return true
	})
	return found
}

// PredHasPrintCall reports whether the tool body calls the print builtin. It
// matches the bare `print` callee only, so pprint() and other callees whose
// text merely contains "print(" are not flagged — the false positive that
// substring matching cannot avoid.
func PredHasPrintCall(t models.ToolDef, pf analysis.ParsedFile) bool {
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
		if astutil.NodeText(fn, pf.Source) == "print" {
			found = true
			return false
		}
		return true
	})
	return found
}

func PredHasWriteCall(t models.ToolDef, pf analysis.ParsedFile) bool {
	// TypeScript tools carry the signal as a discovery-computed fact (set by
	// tsHandlerFacts for writeFile / writeFileSync / createWriteStream /
	// appendFile callees). Branch on language so the Python path is unchanged.
	if t.Language == models.LanguageTypeScript {
		return t.Facts["writes_fs"] == "true"
	}
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
	// TypeScript tools carry the signal as a discovery-computed fact; the
	// Python AST walk below does not understand the TS grammar. Branch on
	// language so the Python path stays byte-identical.
	if t.Language == models.LanguageTypeScript {
		return t.Facts["dynamic_url"] == "true"
	}
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return false
	}
	aliases := analysis.ResolveClientAliases(root, pf.Source)
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "call" {
			return true
		}
		if _, ok := analysis.IsHTTPCallNode(n, pf.Source, aliases); !ok {
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
	var body string
	if root := analysis.FindFunctionNode(t, pf); root != nil {
		body = astutil.NodeText(root, pf.Source)
	} else {
		// No Python function node (the TypeScript case). Fall back to the
		// tool's source span [Line, EndLine], populated by TS discovery. The
		// span covers the whole tool(...) call including its handler, which is
		// what substring rules (fetch(, eval(, execSync, AbortSignal) need.
		body = bodyTextFromSpan(pf.Source, t.Line, t.EndLine)
	}
	for _, needle := range needles {
		if strings.Contains(body, needle) {
			return true
		}
	}
	return false
}

// bodyTextFromSpan returns the 1-based inclusive line range [start, end] of
// src as a string, or "" if the range is invalid. Used when no AST function
// node is available (TypeScript tools carry only line positions).
func bodyTextFromSpan(src []byte, start, end int) string {
	if start <= 0 || end < start {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
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
	aliases := analysis.ResolveClientAliases(root, pf.Source)
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
		// Resolve the canonical callee (direct or aliased). Fall back to raw
		// callee text for non-HTTP callees so the predicate stays usable for
		// any callee list, not only HTTP ones.
		canonical, ok := analysis.IsHTTPCallNode(n, pf.Source, aliases)
		if !ok {
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			canonical = astutil.NodeText(fn, pf.Source)
		}
		if _, want := calleeSet[canonical]; !want {
			return true
		}
		// Fires when the kwarg is absent OR present with literal None (an
		// explicitly-disabled value is the same hazard as a missing one).
		value, present := astutil.KwargValue(n, pf.Source, expr.Missing)
		if !present || value == "None" {
			found = true
		}
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
		kw := lookupKwarg(a, p)
		if kw == nil {
			return true // absent
		}
		if kw.Value != nil && kw.Value.Kind == models.ExprLiteralNone {
			return true // present but explicitly None — ineffective
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

// hostedClassToKind maps hosted-tool classes to the synthetic ToolKind they
// represent. Hosted tools have no ToolDef (and thus no Kind), so without this
// agent_uses_tool_kind:[shell_invocation] would miss an agent wired with a
// hosted ShellTool / CodeInterpreterTool / BashTool — the shapes OAI-101/104
// promise to catch (documented gap (c)).
var hostedClassToKind = map[string]string{
	"ShellTool":           "shell_invocation",
	"LocalShellTool":      "shell_invocation",
	"CodeInterpreterTool": "shell_invocation",
	"ApplyPatchTool":      "shell_invocation",
	"BashTool":            "shell_invocation", // Google ADK
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
			// A decorated tool (Kind openai_tool / claude_sdk_tool /
			// adk_function_tool) that shells out is not Kind shell_invocation,
			// but its body still gives the agent shell reach. Discovery stamps
			// a structural shells_out fact (Python in buildTool, TS in
			// tsHandlerFacts); honor it so shell_invocation also matches the
			// common @function_tool-wraps-subprocess shape.
			if k == "shell_invocation" && ref.Resolved.Facts["shells_out"] == "true" {
				return true
			}
		}
	}
	// Hosted tools carry no ToolDef; map their class to a synthetic kind so a
	// hosted ShellTool etc. still satisfies agent_uses_tool_kind.
	for _, ref := range a.HostedToolRefs {
		hk, ok := hostedClassToKind[ref.Class]
		if !ok {
			continue
		}
		for _, k := range kinds {
			if hk == k {
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

// lookupKwargInTree navigates a dotted path within a KwargTree, returning the
// node at that path or nil. Mirror of lookupKwarg but rooted at any tree (used
// for hosted-tool kwargs, which hang off the HostedToolDef, not the agent).
func lookupKwargInTree(t *models.KwargTree, path string) *models.KwargTree {
	if t == nil {
		return nil
	}
	cur := t
	for _, p := range strings.Split(path, ".") {
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

// PredAgentHostedToolKwargPresent fires when the agent wires a hosted tool of
// the named class whose Kwarg is present. Requires Resolved (the def carries the
// captured kwargs); unresolved refs cannot be inspected.
func PredAgentHostedToolKwargPresent(expr HostedToolKwargExpr, a models.AgentDef) bool {
	for _, ref := range a.HostedToolRefs {
		if ref.Class != expr.Class || ref.Resolved == nil {
			continue
		}
		if lookupKwargInTree(ref.Resolved.Kwargs, expr.Kwarg) != nil {
			return true
		}
	}
	return false
}

// PredAgentHostedToolKwargValue fires when the agent wires a hosted tool of the
// named class whose Kwarg equals Value (quote-stripped for string literals).
func PredAgentHostedToolKwargValue(expr HostedToolKwargValueExpr, a models.AgentDef) bool {
	for _, ref := range a.HostedToolRefs {
		if ref.Class != expr.Class || ref.Resolved == nil {
			continue
		}
		kw := lookupKwargInTree(ref.Resolved.Kwargs, expr.Kwarg)
		if kw == nil || kw.Value == nil {
			continue
		}
		raw := kw.Value.Text
		if kw.Value.Kind == models.ExprLiteralString {
			raw = strings.Trim(raw, `"'`)
		}
		if raw == expr.Value {
			return true
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

// ─── subagent predicates ──────────────────────────────────────────────────────

// PredSubagentGrantsTool reports whether the subagent grants any of names.
// It matches against the parsed ToolGrants first (so a parametered grant like
// "Bash(npm run *)" matches the name "Bash") and falls back to the raw Tools
// tokens (so hand-built SubagentDefs that set only Tools still match).
// Case-sensitive: Claude Code tool names are canonical ("Bash", "Read", ...).
func PredSubagentGrantsTool(s models.SubagentDef, names []string) bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for _, g := range s.ToolGrants {
		if want[g.Tool] {
			return true
		}
	}
	for _, granted := range s.Tools {
		if want[granted] {
			return true
		}
	}
	return false
}

// ─── repo predicates ──────────────────────────────────────────────────────────

func PredRepoHasSDKInCode(sdks []string, inv models.RepoInventory) bool {
	for _, want := range sdks {
		// "openshell" is a risk-surface label, not an SDK — see models.go.
		if want == "openshell" {
			if inv.HasShellInvocations {
				return true
			}
			continue
		}
		for _, s := range inv.SDKsDetected {
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

// PredRepoClaudeDefaultModeIs fires when any discovered .claude/settings.json
// (or settings.local.json) declares a defaultMode equal to one of the listed
// modes. The defaultMode governs how Claude Code asks for permission before
// running tools; values like "bypassPermissions" disable the prompts entirely,
// turning every granted tool into an unguarded capability for the whole repo.
func PredRepoClaudeDefaultModeIs(modes []string, inv models.RepoInventory) bool {
	for _, cs := range inv.ClaudeSettings {
		for _, m := range modes {
			if cs.DefaultMode == m {
				return true
			}
		}
	}
	return false
}

// PredRepoClaudeOptionsPermissionModeIs fires when any discovered
// ClaudeAgentOptions(...) construction sets permission_mode to one of the listed
// modes. This is the session-level (in-code) analogue of
// repo_claude_default_mode_is: ClaudeAgentOptions(permission_mode="bypassPermissions")
// disables Claude Code's approval prompts for the session the same way
// .claude/settings.json defaultMode does for the project.
func PredRepoClaudeOptionsPermissionModeIs(modes []string, inv models.RepoInventory) bool {
	for _, opt := range inv.ClaudeAgentOptions {
		node := lookupKwargInTree(opt.Kwargs, "permission_mode")
		if node == nil || node.Value == nil {
			continue
		}
		val := node.Value.Text
		if node.Value.Kind == models.ExprLiteralString {
			val = strings.Trim(val, `"'`)
		}
		for _, m := range modes {
			if val == m {
				return true
			}
		}
	}
	return false
}
