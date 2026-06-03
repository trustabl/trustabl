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
// contributor sees every problem in one run.
//
// Recursive walk supports the by-category directory layout
// (e.g. policies/claude_sdk/*.yaml, policies/openshell/*.yaml). The flat
// layout still works — fs.WalkDir at "." is a strict superset of fs.Glob.
func Load(fsys fs.FS) ([]PolicyFile, error) {
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
		// manifest.yaml at the pack root carries schema metadata, not a
		// policy. The rulesource package reads it; the loader skips it.
		if !d.IsDir() && strings.HasSuffix(path, ".yaml") && path != "manifest.yaml" {
			entries = append(entries, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	// Sort for deterministic load order — matters for the duplicate-rule-ID
	// "previously defined in" message and for any stable iteration downstream.
	sort.Strings(entries)

	var (
		policies []PolicyFile
		errs     []error
		seenIDs  = map[string]string{} // rule ID → file that defined it
	)

	for _, name := range entries {
		// Per-file work in a closure so the file is closed at the end of each
		// iteration, not deferred to the end of Load (which would leak fds
		// proportional to the number of policy files).
		pf, decErr := decodePolicyFile(fsys, name)
		if decErr != nil {
			errs = append(errs, decErr)
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
		if pf.Policy.Category != "" {
			switch pf.Policy.Category {
			case models.CategoryClaudeSDK, models.CategoryOpenAISDK,
				models.CategoryOpenShell, models.CategoryGoogleADK:
				// valid
			default:
				errs = append(errs, fmt.Errorf("%s: unknown category %q (allowed: claude_sdk, openai_sdk, openshell, google_adk)", name, pf.Policy.Category))
			}
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
			if rule.Language != "" {
				switch rule.Language {
				case models.LanguagePython, models.LanguageTypeScript,
					models.LanguageJavaScript, models.LanguageGo:
					// valid
				default:
					errs = append(errs, fmt.Errorf("%s: unknown language %q (allowed: python, typescript, javascript, go)", tag, rule.Language))
				}
			}
			if rule.Scope == "" {
				errs = append(errs, fmt.Errorf("%s: scope is required (tool|agent|repo|subagent)", tag))
			} else if !models.ValidScope(rule.Scope) {
				errs = append(errs, fmt.Errorf("%s: unknown scope %q (allowed: tool, agent, repo, subagent)", tag, rule.Scope))
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
					errs = append(errs, fmt.Errorf("%s: repo_has_sdk_in_code value %q is not a known SDK token (want one of: claude_agent_sdk, openai_agents, google_adk, mcp, openshell)", tag, sdk))
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
		return nil, errors.Join(errs...)
	}
	return policies, nil
}

// validRepoHasSDKInCode reports whether tok is a value RepoInventory.SDKsDetected
// can actually hold. These are the SDK-enum tokens (plus the "openshell"
// risk-surface label handled specially by PredRepoHasSDKInCode), deliberately
// distinct from the category tokens accepted by applies_to at repo scope.
func validRepoHasSDKInCode(tok string) bool {
	switch tok {
	case string(models.SDKClaudeAgentSDK), string(models.SDKOpenAIAgents),
		string(models.SDKGoogleADK), string(models.SDKMCP), "openshell":
		return true
	}
	return false
}

func validAppliesToForScope(scope models.Scope, kind string) bool {
	switch scope {
	case models.ScopeTool:
		switch kind {
		case "claude_sdk_tool", "openai_tool", "mcp_tool",
			"shell_invocation", "unknown", "adk_function_tool":
			return true
		}
	case models.ScopeAgent:
		switch kind {
		case "openai_agent", "openai_sandbox_agent", "claude_agent_definition",
			"claude_query_main",
			"adk_llm_agent", "adk_sequential_agent", "adk_parallel_agent",
			"adk_loop_agent", "adk_langgraph_agent":
			return true
		}
	case models.ScopeRepo:
		switch kind {
		case "claude_sdk", "openai_agents", "openshell", "mcp", "google_adk":
			return true
		}
	case models.ScopeSubagent:
		switch kind {
		case "claude_subagent":
			return true
		}
	}
	return false
}

// decodePolicyFile opens, decodes, and closes one YAML file from fsys.
// Returning errors as values rather than holding the fd open via defer keeps
// the descriptor budget bounded by Load's iteration, not by total policy count.
func decodePolicyFile(fsys fs.FS, name string) (PolicyFile, error) {
	var pf PolicyFile
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
