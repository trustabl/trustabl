package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

// Load walks fsys recursively, decodes every .yaml file, validates it, and
// returns the collected policies. All errors are batched (not fail-fast) so a
// contributor sees every problem in one run. This is the STRICT path
// (KnownFields(true)): an unknown YAML key fails the load, so authoring typos
// and bad rules are caught. Used by tests/CI and any caller validating a pack
// authored against THIS engine build.
//
// Recursive walk supports the by-category directory layout
// (e.g. policies/claude_sdk/*.yaml, policies/openshell/*.yaml). The flat
// layout still works — fs.WalkDir at "." is a strict superset of fs.Glob.
func Load(fsys fs.FS) ([]PolicyFile, error) {
	policies, _, err := loadPolicies(fsys, false)
	return policies, err
}

// LoadLenient is the forward-compatible runtime path. It behaves like Load but
// tolerates a rules pack from a NEWER schema than this build: a rule whose
// scope, applies_to value, or match predicate this engine does not understand
// is dropped whole (its ID collected into the returned skipped slice) instead
// of failing the entire pack. This lets a deployed binary degrade gracefully
// against an updated rules repo — scanning with the rules it understands —
// rather than refusing to run. All other validation stays strict, so a genuine
// defect in a rule this build CAN evaluate (a missing required field, an
// out-of-range confidence, a duplicate ID, a degenerate match) still fails the
// load. Forward-incompatibility is narrow by design: an EMPTY scope/applies_to
// is a missing required field, not a newer-engine signal, so it still hard-fails.
func LoadLenient(fsys fs.FS) ([]PolicyFile, []string, error) {
	return loadPolicies(fsys, true)
}

// loadPolicies is the shared implementation behind Load (lenient=false) and
// LoadLenient (lenient=true). When lenient, the second return holds the IDs of
// rules dropped as forward-incompatible (see decodePolicyFileLenient).
func loadPolicies(fsys fs.FS, lenient bool) ([]PolicyFile, []string, error) {
	var entries []string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Never descend a VCS metadata directory. A cached pack is a git clone,
		// so its root contains a .git/ tree; any *.yaml that happens to live
		// under it is repo plumbing, not a policy, and must not be loaded as a
		// rule. Skip the whole subtree.
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		// Never load a symlink. os.DirFS follows symlinks on Open, so a hostile
		// rules pack (a custom --rules-repo / TRUSTABL_RULES_REPO) containing e.g.
		// rules/x.yaml -> /etc/passwd would otherwise read outside the pack
		// directory and feed host-filesystem content into the loader. fs.WalkDir
		// does not descend symlinked directories, so skipping the entry suffices.
		// This is the single chokepoint covering both clone paths (plumbing fetch
		// and PlainClone fallback) and any cached pack.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		// manifest.yaml at the pack root carries schema metadata, not a
		// policy. The rulesource package reads it; the loader skips it.
		if !d.IsDir() && strings.HasSuffix(path, ".yaml") && path != "manifest.yaml" {
			entries = append(entries, path)
		}
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("walk: %w", err)
	}
	// Sort for deterministic load order — matters for the duplicate-rule-ID
	// "previously defined in" message and for any stable iteration downstream.
	sort.Strings(entries)

	var (
		policies []PolicyFile
		errs     []error
		skipped  []string              // forward-incompatible rule IDs (lenient only)
		seenIDs  = map[string]string{} // rule ID → file that defined it
	)
	var known map[string]bool
	if lenient {
		known = knownMatchKeys()
	}

	for _, name := range entries {
		// Per-file work in a closure so the file is closed at the end of each
		// iteration, not deferred to the end of Load (which would leak fds
		// proportional to the number of policy files).
		var (
			pf       PolicyFile
			fileSkip []string
			decErr   error
		)
		if lenient {
			pf, fileSkip, decErr = decodePolicyFileLenient(fsys, name, known)
		} else {
			pf, decErr = decodePolicyFile(fsys, name)
		}
		if decErr != nil {
			errs = append(errs, decErr)
			continue
		}
		skipped = append(skipped, fileSkip...)

		// Forward-compatible category gate. An unrecognized (non-empty) category is
		// a pack for an SDK that a newer rules release added but this build does not
		// audit yet. In lenient (runtime) mode, skip the whole file — its rules are
		// recorded as skipped and surfaced like any forward-incompatible rule —
		// rather than hard-failing the entire load and blocking every other SDK's
		// rules; this is what stops a new category from forcing a lockstep binary
		// upgrade. Strict mode (authoring/CI) still rejects it so a typo'd category
		// is caught before shipping. Checked before the required-field validation so
		// an unknown-category pack is skipped cleanly without also reporting its
		// other fields.
		if pf.Policy.Category != "" && !models.ValidCategory(pf.Policy.Category) {
			if lenient {
				for _, r := range pf.Rules {
					if r.ID != "" {
						skipped = append(skipped, r.ID)
					}
				}
				continue
			}
			errs = append(errs, fmt.Errorf("%s: unknown category %q (allowed: claude_sdk, openai_sdk, openshell, google_adk, mcp, langchain, crewai, pydantic_ai, vercel_ai, autogen)", name, pf.Policy.Category))
			continue
		}

		// Validate policy-level required fields.
		policyErrCount := len(errs)
		if pf.Policy.ID == "" {
			errs = append(errs, fmt.Errorf("%s: policy.id is required", name))
		}
		if pf.Policy.Category == "" {
			errs = append(errs, fmt.Errorf("%s: policy.category is required", name))
		}
		if len(errs) > policyErrCount {
			continue
		}

		for i, rule := range pf.Rules {
			tag := fmt.Sprintf("%s rule[%d]", name, i)
			if rule.ID != "" {
				tag = fmt.Sprintf("%s rule %s", name, rule.ID)
			}
			if rule.ID == "" {
				errs = append(errs, fmt.Errorf("%s: id is required", tag))
			}
			if rule.Title == "" {
				errs = append(errs, fmt.Errorf("%s: title is required", tag))
			}
			if rule.Severity == "" {
				errs = append(errs, fmt.Errorf("%s: severity is required", tag))
			}
			if rule.Severity != "" {
				switch rule.Severity {
				case models.SeverityInfo, models.SeverityLow, models.SeverityMedium,
					models.SeverityHigh, models.SeverityCritical:
					// valid
				default:
					errs = append(errs, fmt.Errorf("%s: unknown severity %q (allowed: info, low, medium, high, critical)", tag, rule.Severity))
				}
			}
			if rule.Confidence <= 0 || math.IsNaN(rule.Confidence) {
				// NaN fails every ordered comparison, so it would slip past a
				// bare `<= 0 || > 1` range check and then poison scoring
				// (NaN * weight = NaN) and the deterministic finding order.
				errs = append(errs, fmt.Errorf("%s: confidence is required (must be a number > 0)", tag))
			} else if rule.Confidence > 1 {
				// confidence is a probability in (0, 1]; scoring multiplies it by
				// severity weight, so a value above 1 silently inflates the score.
				errs = append(errs, fmt.Errorf("%s: confidence %g is out of range (must be <= 1)", tag, rule.Confidence))
			}
			if len(rule.AppliesTo) == 0 {
				errs = append(errs, fmt.Errorf("%s: applies_to is required", tag))
			}
			if rule.Explanation == "" {
				errs = append(errs, fmt.Errorf("%s: explanation is required", tag))
			}
			if rule.Fix == "" {
				errs = append(errs, fmt.Errorf("%s: fix is required", tag))
			}
			if rule.ID != "" {
				if prev, seen := seenIDs[rule.ID]; seen {
					errs = append(errs, fmt.Errorf("duplicate rule ID %q in %s (previously defined in %s)", rule.ID, name, prev))
				} else {
					seenIDs[rule.ID] = name
				}
			}
			// An explicit, unrecognized language is rejected in strict (authoring)
			// mode so a typo is caught in CI. The lenient runtime path never reaches
			// here for such a rule — decodePolicyFileLenient drops it as
			// forward-incompatible (see ruleNeedsNewerEngine) — so a newer rules
			// release does not break an older binary. Empty is fine (defaults to
			// python). models.AllLanguages is the single source of truth.
			if rule.Language != "" && !models.ValidLanguage(rule.Language) {
				errs = append(errs, fmt.Errorf("%s: unknown language %q (allowed: %s)", tag, rule.Language, languageAllowList()))
			}
			if rule.Scope == "" {
				errs = append(errs, fmt.Errorf("%s: scope is required (tool|agent|repo|subagent)", tag))
			} else if !models.ValidScope(rule.Scope) {
				errs = append(errs, fmt.Errorf("%s: unknown scope %q (allowed: tool, agent, repo, subagent)", tag, rule.Scope))
			}
			// Reject a match nested beyond the depth bound BEFORE any recursive
			// walk (outOfScopePredicates / degenerateCombinators below, or the
			// evaluator at scan time) touches it — a hostile pack could otherwise
			// exhaust the stack. exceedsMatchDepth is itself depth-bounded, so it
			// cannot overflow while measuring.
			if exceedsMatchDepth(rule.Match, maxMatchDepth) {
				errs = append(errs, fmt.Errorf("%s: match nesting exceeds the maximum depth of %d", tag, maxMatchDepth))
				continue
			}
			// Scope-DEPENDENT checks: these need a known scope to evaluate
			// (validAppliesToForScope and outOfScopePredicates both take the
			// scope as input), so they can only run once the scope is valid. A
			// rule with a broken scope surfaces its predicate errors on the next
			// run, after the scope typo is fixed.
			if models.ValidScope(rule.Scope) {
				for _, kind := range rule.AppliesTo {
					if !validAppliesToForScope(rule.Scope, kind) {
						errs = append(errs, fmt.Errorf("%s: applies_to value %q is not valid for scope %q", tag, kind, rule.Scope))
					}
				}
				// A predicate the scope's evaluator never dispatches would be
				// silently dropped at match time, so the rule would fire more
				// broadly than written. Reject it here instead.
				if bad := rule.Match.outOfScopePredicates(rule.Scope); len(bad) > 0 {
					errs = append(errs, fmt.Errorf("%s: match predicate(s) [%s] are not valid for scope %q", tag, strings.Join(bad, ", "), rule.Scope))
				}
				// An empty top-level match is vacuously true, so it fires on EVERY
				// instance of its applies_to kinds. That is only meaningful at repo
				// scope (the "fires once, gated solely by applies_to" singleton
				// pattern). At a per-instance scope it is a finding-spam footgun —
				// almost always a forgotten predicate — so reject it rather than
				// flooding every tool/agent/subagent with the finding.
				if rule.Scope != models.ScopeRepo && rule.Match.isEmpty() {
					errs = append(errs, fmt.Errorf("%s: empty match is only allowed at repo scope; a %s-scoped rule with no predicate fires on every %s", tag, rule.Scope, rule.Scope))
				}
			}
			// Scope-INDEPENDENT checks: these do not depend on the rule's scope,
			// so they run unconditionally — a bad token or degenerate combinator
			// must still be reported when the scope is missing/invalid, honoring
			// the batch-all-errors-in-one-run intent.
			//
			// repo_has_sdk_in_code matches RepoInventory.SDKsDetected, which holds
			// SDK-enum tokens (claude_agent_sdk, openai_agents, ...) — NOT the
			// category tokens used by applies_to (claude_sdk, ...). A category
			// token here silently never matches, so the rule never fires.
			for _, sdk := range rule.Match.repoSDKInCodeValues() {
				if !validRepoHasSDKInCode(sdk) {
					errs = append(errs, fmt.Errorf("%s: repo_has_sdk_in_code value %q is not a known SDK token (want one of: claude_agent_sdk, openai_agents, google_adk, mcp, langchain, openshell)", tag, sdk))
				}
			}
			// A degenerate combinator (empty any/all list, or not: over an empty
			// expression) is an authoring mistake, not intent.
			if degen := rule.Match.degenerateCombinators(); len(degen) > 0 {
				errs = append(errs, fmt.Errorf("%s: degenerate match combinator(s): %s", tag, strings.Join(degen, ", ")))
			}
			// Populate category from policy metadata — not in YAML.
			pf.Rules[i].Category = models.DetectorCategory(pf.Policy.Category)
			// Default language to python ONLY for tool/agent scope (the
			// AST-backed scopes). Subagent rules audit markdown frontmatter and
			// repo rules audit the inventory — neither gates on language, so an
			// omitted language stays empty.
			if pf.Rules[i].Language == "" &&
				(pf.Rules[i].Scope == models.ScopeTool || pf.Rules[i].Scope == models.ScopeAgent) {
				pf.Rules[i].Language = models.LanguagePython
			}
		}
		policies = append(policies, pf)
	}

	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return policies, skipped, nil
}

// languageAllowList renders the recognized rule languages for the strict
// validation error, sourced from models.AllLanguages so it never drifts from
// what the loader actually accepts.
func languageAllowList() string {
	names := make([]string, len(models.AllLanguages))
	for i, l := range models.AllLanguages {
		names[i] = string(l)
	}
	return strings.Join(names, ", ")
}

// validRepoHasSDKInCode reports whether tok is a value RepoInventory.SDKsDetected
// can actually hold. These are the SDK-enum tokens (plus the "openshell"
// risk-surface label handled specially by PredRepoHasSDKInCode), deliberately
// distinct from the category tokens accepted by applies_to at repo scope.
func validRepoHasSDKInCode(tok string) bool {
	switch tok {
	case string(models.SDKClaudeAgentSDK), string(models.SDKOpenAIAgents),
		string(models.SDKGoogleADK), string(models.SDKMCP),
		string(models.SDKLangChain), "openshell",
		string(models.SDKCrewAI), string(models.SDKPydanticAI),
		string(models.SDKVercelAI), string(models.SDKAutoGen):
		return true
	}
	return false
}

// appliesToByScope is the single source of truth for which applies_to values are
// valid at each scope. validAppliesToForScope checks against it, and the
// capability descriptor (AppliesToByScope / trustabl capabilities) enumerates it.
// Order is stable for deterministic descriptor output.
var appliesToByScope = map[models.Scope][]string{
	models.ScopeTool: {
		"claude_sdk_tool", "openai_tool", "mcp_tool",
		"shell_invocation", "unknown", "adk_function_tool",
		"langchain_tool",
		"crewai_tool", "pydantic_ai_tool", "vercel_ai_tool", "autogen_tool",
	},
	models.ScopeAgent: {
		"openai_agent", "openai_sandbox_agent", "claude_agent_definition",
		"claude_query_main",
		"adk_llm_agent", "adk_sequential_agent", "adk_parallel_agent",
		"adk_loop_agent", "adk_langgraph_agent",
		"langchain_agent", "langchain_agent_executor", "langchain_state_graph",
		"crewai_agent", "pydantic_ai_agent", "vercel_ai_agent",
		"autogen_conversable_agent", "autogen_user_proxy_agent",
		"autogen_assistant_agent", "autogen_group_chat_manager",
		"autogen_code_executor_agent",
	},
	models.ScopeRepo: {
		"claude_sdk", "openai_agents", "openshell", "mcp", "google_adk",
		"langchain",
		"crewai", "pydantic_ai", "vercel_ai", "autogen",
	},
	models.ScopeSubagent: {"claude_subagent"},
}

func validAppliesToForScope(scope models.Scope, kind string) bool {
	for _, k := range appliesToByScope[scope] {
		if k == kind {
			return true
		}
	}
	return false
}

// AppliesToByScope returns a fresh copy of the valid applies_to values per scope
// for the capability descriptor. Returns a copy so a caller cannot mutate the
// source of truth.
func AppliesToByScope() map[models.Scope][]string {
	out := make(map[models.Scope][]string, len(appliesToByScope))
	for s, kinds := range appliesToByScope {
		out[s] = append([]string(nil), kinds...)
	}
	return out
}

// maxRuleFileBytes caps an individual rule YAML file. The rules pack is cloned
// from a git repo operators can override (--rules-repo / TRUSTABL_RULES_REPO), so
// a hostile pack could ship a giant YAML to OOM the loader (the lenient path
// holds the raw bytes AND a full yaml.Node tree at once). Real rule files are a
// few KiB; 4 MiB is generous headroom.
const maxRuleFileBytes = 4 << 20

// checkRuleFileSize rejects a rule file larger than maxRuleFileBytes before it is
// read into memory. fs.Stat falls back to Open+Stat for any fs.FS, so this works
// for the production os.DirFS and for in-memory test filesystems alike; a stat
// error is left for the subsequent read to surface with a clearer message.
func checkRuleFileSize(fsys fs.FS, name string) error {
	fi, err := fs.Stat(fsys, name)
	if err != nil {
		return nil
	}
	if fi.Size() > maxRuleFileBytes {
		return fmt.Errorf("%s: rule file is %d bytes, exceeds the %d-byte limit", name, fi.Size(), int64(maxRuleFileBytes))
	}
	return nil
}

// decodePolicyFile opens, decodes, and closes one YAML file from fsys.
// Returning errors as values rather than holding the fd open via defer keeps
// the descriptor budget bounded by Load's iteration, not by total policy count.
func decodePolicyFile(fsys fs.FS, name string) (PolicyFile, error) {
	var pf PolicyFile
	if err := checkRuleFileSize(fsys, name); err != nil {
		return pf, err
	}
	f, err := fsys.Open(name)
	if err != nil {
		return pf, fmt.Errorf("%s: open: %w", name, err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return pf, fmt.Errorf("%s: decode: %w", name, err)
	}
	return pf, nil
}

// decodePolicyFileLenient decodes one YAML policy file in forward-compatible
// mode. It parses to a yaml.Node (malformed YAML is still a hard error),
// identifies rules that reference a scope, applies_to value, or match predicate
// this build does not understand (a newer rule schema this build cannot
// evaluate — see ruleNeedsNewerEngine), decodes the file leniently (unknown keys
// ignored — additive forward-compat for non-match fields too), and drops the
// forward-incompatible rules wholesale. Returns the dropped rule IDs. Dropping
// the WHOLE rule is essential: a lenient struct decode silently omits the
// unknown predicate, which would collapse the rule's match to vacuous-true
// (firing on every entity) — so the rule must be removed, not half-decoded.
func decodePolicyFileLenient(fsys fs.FS, name string, known map[string]bool) (PolicyFile, []string, error) {
	var pf PolicyFile
	if err := checkRuleFileSize(fsys, name); err != nil {
		return pf, nil, err
	}
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return pf, nil, fmt.Errorf("%s: open: %w", name, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return pf, nil, fmt.Errorf("%s: decode: %w", name, err)
	}
	skipIdx, skipIDs := forwardIncompatibleRules(&root, known, name)
	if err := root.Decode(&pf); err != nil {
		return pf, nil, fmt.Errorf("%s: decode: %w", name, err)
	}
	if len(skipIdx) > 0 {
		pf.Rules = dropRulesAt(pf.Rules, skipIdx)
	}
	return pf, skipIDs, nil
}

// forwardIncompatibleRules returns the indices (into the rules sequence, which
// align 1:1 with pf.Rules after a lenient decode) and IDs of rules that
// reference a scope, applies_to value, or match predicate outside this build's
// vocabulary — i.e. rules authored against a newer engine (see
// ruleNeedsNewerEngine). fileName labels rules missing an id.
func forwardIncompatibleRules(root *yaml.Node, known map[string]bool, fileName string) ([]int, []string) {
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	rulesNode := mappingValue(doc, "rules")
	if rulesNode == nil || rulesNode.Kind != yaml.SequenceNode {
		return nil, nil
	}
	var (
		idx []int
		ids []string
	)
	for i, ruleNode := range rulesNode.Content {
		if !ruleNeedsNewerEngine(ruleNode, known) {
			continue
		}
		idx = append(idx, i)
		id := ""
		if idNode := mappingValue(ruleNode, "id"); idNode != nil {
			id = idNode.Value
		}
		if id == "" {
			id = fmt.Sprintf("%s rule[%d]", fileName, i)
		}
		ids = append(ids, id)
	}
	return idx, ids
}

// ruleNeedsNewerEngine reports whether a rule node references a scope,
// applies_to value, or match predicate this build does not understand — the
// signal that the rule was authored against a newer engine and so must be
// skipped (not evaluated) by the lenient runtime loader. The three checks mirror
// the strict validation in loadPolicies, but here an unknown value means "skip"
// instead of "fail".
//
// Crucially, an EMPTY scope or applies_to is NOT forward-incompatible: it is a
// missing required field — a real authoring defect — which the strict validation
// loop must still hard-fail in BOTH modes (this is what the ticket means by "not
// a license to silently drop real authoring errors"). So each check fires only
// on a NON-empty, unrecognized value, never on absence.
func ruleNeedsNewerEngine(ruleNode *yaml.Node, known map[string]bool) bool {
	// Unknown scope: a scope kind (e.g. a future `skill`) this build lacks. An
	// unknown scope also makes applies_to unvalidatable (validAppliesToForScope
	// keys off the scope), so it is checked first and short-circuits.
	scope := models.Scope(scalarValue(ruleNode, "scope"))
	if scope != "" && !models.ValidScope(scope) {
		return true
	}
	// Unknown applies_to value for an otherwise-known scope: a tool/agent kind a
	// newer engine added. Only meaningful once the scope is known (above).
	if scope != "" {
		for _, kind := range sequenceValues(ruleNode, "applies_to") {
			if !validAppliesToForScope(scope, kind) {
				return true
			}
		}
	}
	// Unknown language: a source language (e.g. a future `ruby`) whose discovery
	// this build lacks, so a rule targeting it can never match and was authored
	// against a newer engine. Empty is NOT incompatible — it defaults to python (a
	// known language), mirroring the empty-scope / empty-applies_to carve-out: an
	// absent field is a default, only a present-but-unrecognized value is a
	// newer-engine signal.
	lang := models.Language(scalarValue(ruleNode, "language"))
	if lang != "" && !models.ValidLanguage(lang) {
		return true
	}
	// Unknown predicate key anywhere in the match tree: a predicate from a newer
	// schema. A known predicate's nested struct keys are NOT match-level keys, so
	// matchHasUnknownKey does not descend into them (see its doc).
	if matchNode := mappingValue(ruleNode, "match"); matchNode != nil && matchNode.Kind == yaml.MappingNode {
		if matchHasUnknownKey(matchNode, known, 0) {
			return true
		}
	}
	return false
}

// matchHasUnknownKey reports whether a match mapping (recursing through the
// all/any/not combinators) references any key not in `known`. A leaf predicate
// key not in `known` is a predicate from a newer schema; its value is NOT
// descended into (a known predicate's nested struct keys, e.g.
// param_name_matches.exact, are not match-level keys).
func matchHasUnknownKey(m *yaml.Node, known map[string]bool, depth int) bool {
	// Bound recursion: a pathologically nested match in a hostile rules pack
	// would otherwise exhaust the stack during this forward-compat pre-pass.
	// Treat over-deep nesting as "unknown" so the rule is dropped, not crashed.
	if depth > maxMatchDepth {
		return true
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i].Value
		val := m.Content[i+1]
		if !known[key] {
			return true
		}
		switch key {
		case "all", "any":
			if val.Kind == yaml.SequenceNode {
				for _, el := range val.Content {
					if el.Kind == yaml.MappingNode && matchHasUnknownKey(el, known, depth+1) {
						return true
					}
				}
			}
		case "not":
			if val.Kind == yaml.MappingNode && matchHasUnknownKey(val, known, depth+1) {
				return true
			}
		}
	}
	return false
}

// mappingValue returns the value node for key in a YAML mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// scalarValue returns the scalar string at key in a YAML mapping node, or "".
func scalarValue(m *yaml.Node, key string) string {
	if v := mappingValue(m, key); v != nil {
		return v.Value
	}
	return ""
}

// sequenceValues returns the scalar values of the sequence at key in a YAML
// mapping node, or nil if the key is absent or not a sequence.
func sequenceValues(m *yaml.Node, key string) []string {
	v := mappingValue(m, key)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(v.Content))
	for _, el := range v.Content {
		out = append(out, el.Value)
	}
	return out
}

// dropRulesAt returns rules with the entries at the given indices removed.
func dropRulesAt(rules []RuleDef, idx []int) []RuleDef {
	skip := make(map[int]bool, len(idx))
	for _, i := range idx {
		skip[i] = true
	}
	out := make([]RuleDef, 0, len(rules)-len(idx))
	for i, r := range rules {
		if !skip[i] {
			out = append(out, r)
		}
	}
	return out
}
