package rules

import (
	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// EvaluateAgent walks a MatchExpr against an agent. Agent predicates are
// dispatched here; tool predicates return true vacuously.
func (e MatchExpr) EvaluateAgent(a models.AgentDef, inv models.RepoInventory) bool {
	if len(e.All) > 0 {
		for _, sub := range e.All {
			if !sub.EvaluateAgent(a, inv) {
				return false
			}
		}
	}
	if len(e.Any) > 0 {
		matched := false
		for _, sub := range e.Any {
			if sub.EvaluateAgent(a, inv) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if e.Not != nil && e.Not.EvaluateAgent(a, inv) {
		return false
	}
	if e.Always != nil && !*e.Always {
		return false
	}
	if len(e.AgentClass) > 0 && !PredAgentClass(e.AgentClass, a) {
		return false
	}
	if len(e.AgentKwargPresent) > 0 && !PredAgentKwargPresent(e.AgentKwargPresent, a) {
		return false
	}
	if len(e.AgentKwargMissing) > 0 && !PredAgentKwargMissing(e.AgentKwargMissing, a) {
		return false
	}
	if len(e.AgentKwargListEmpty) > 0 && !PredAgentKwargListEmpty(e.AgentKwargListEmpty, a) {
		return false
	}
	if e.AgentKwargValue != nil && !PredAgentKwargValue(*e.AgentKwargValue, a) {
		return false
	}
	if len(e.AgentUsesToolKind) > 0 && !PredAgentUsesToolKind(e.AgentUsesToolKind, a, inv) {
		return false
	}
	if len(e.AgentGrantsBuiltinTool) > 0 && !PredAgentGrantsBuiltinTool(e.AgentGrantsBuiltinTool, a) {
		return false
	}
	if len(e.AgentHandoffToClass) > 0 && !PredAgentHandoffToClass(e.AgentHandoffToClass, a) {
		return false
	}
	if len(e.AgentUsesHostedToolClass) > 0 && !PredAgentUsesHostedToolClass(e.AgentUsesHostedToolClass, a) {
		return false
	}
	if e.AgentIsSubagentOfAny != nil && PredAgentIsSubagentOfAny(a, inv) != *e.AgentIsSubagentOfAny {
		return false
	}
	return true
}

// EvaluateRepo walks a MatchExpr against the repo profile and inventory.
func (e MatchExpr) EvaluateRepo(p models.RepoProfile, inv models.RepoInventory) bool {
	if len(e.All) > 0 {
		for _, sub := range e.All {
			if !sub.EvaluateRepo(p, inv) {
				return false
			}
		}
	}
	if len(e.Any) > 0 {
		matched := false
		for _, sub := range e.Any {
			if sub.EvaluateRepo(p, inv) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if e.Not != nil && e.Not.EvaluateRepo(p, inv) {
		return false
	}
	if e.Always != nil && !*e.Always {
		return false
	}
	if len(e.RepoHasSDKDep) > 0 && !PredRepoHasSDKDep(e.RepoHasSDKDep, p) {
		return false
	}
	if len(e.RepoHasSDKInCode) > 0 && !PredRepoHasSDKInCode(e.RepoHasSDKInCode, inv) {
		return false
	}
	if len(e.RepoHasAgentClass) > 0 && !PredRepoHasAgentClass(e.RepoHasAgentClass, inv) {
		return false
	}
	if len(e.RepoHasNoAgentClass) > 0 && !PredRepoHasNoAgentClass(e.RepoHasNoAgentClass, inv) {
		return false
	}
	if len(e.RepoComponentPresent) > 0 && !PredRepoComponentPresent(e.RepoComponentPresent, p) {
		return false
	}
	if e.RepoUsesDefaultTracing != nil && !PredRepoUsesDefaultTracing(*e.RepoUsesDefaultTracing, inv) {
		return false
	}
	return true
}

// EvaluateSubagent walks a MatchExpr against a subagent. Subagent predicates
// are dispatched here; predicates for other scopes return true vacuously
// (a subagent rule should only set subagent predicates + combinators).
func (e MatchExpr) EvaluateSubagent(s models.SubagentDef, inv models.RepoInventory) bool {
	if len(e.All) > 0 {
		for _, sub := range e.All {
			if !sub.EvaluateSubagent(s, inv) {
				return false
			}
		}
	}
	if len(e.Any) > 0 {
		matched := false
		for _, sub := range e.Any {
			if sub.EvaluateSubagent(s, inv) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if e.Not != nil && e.Not.EvaluateSubagent(s, inv) {
		return false
	}
	if e.Always != nil && !*e.Always {
		return false
	}
	if len(e.SubagentGrantsTool) > 0 && !PredSubagentGrantsTool(s, e.SubagentGrantsTool) {
		return false
	}
	return true
}

// EvaluateTool walks a MatchExpr against a tool and returns whether it matches.
//
// Semantics:
//   - An empty MatchExpr returns true (vacuously matches).
//   - Every set field on a node contributes one boolean to a conjunction
//     (logical AND): all combinators, all primitives, and all nested
//     struct predicates that are non-nil must hold for the node to match.
//   - The combinators `all` and `any` recurse; `not` negates.
func (e MatchExpr) EvaluateTool(t models.ToolDef, pf analysis.ParsedFile) bool {
	// Combinators
	if len(e.All) > 0 {
		for _, sub := range e.All {
			if !sub.EvaluateTool(t, pf) {
				return false
			}
		}
	}
	if len(e.Any) > 0 {
		matched := false
		for _, sub := range e.Any {
			if sub.EvaluateTool(t, pf) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if e.Not != nil && e.Not.EvaluateTool(t, pf) {
		return false
	}

	// Bool predicates — pointer field distinguishes "set to false" from "absent".
	if e.HasDocstring != nil && PredHasDocstring(t) != *e.HasDocstring {
		return false
	}
	if e.HasParams != nil && PredHasParams(t) != *e.HasParams {
		return false
	}
	if e.HasTypedParams != nil && PredHasTypedParams(t) != *e.HasTypedParams {
		return false
	}
	if e.HasRaise != nil && PredHasRaise(t, pf) != *e.HasRaise {
		return false
	}
	if e.HasTryExcept != nil && PredHasTryExcept(t, pf) != *e.HasTryExcept {
		return false
	}
	if e.HasShellCall != nil && PredHasShellCall(t, pf) != *e.HasShellCall {
		return false
	}
	if e.HasCodeExecCall != nil && PredHasCodeExecCall(t, pf) != *e.HasCodeExecCall {
		return false
	}
	if e.HasWriteCall != nil && PredHasWriteCall(t, pf) != *e.HasWriteCall {
		return false
	}
	if e.HasDynamicURLCall != nil && PredHasDynamicURLCall(t, pf) != *e.HasDynamicURLCall {
		return false
	}
	if e.Always != nil && !*e.Always {
		// `always: false` matches nothing; `always: true` is a no-op (the
		// vacuous-true semantics already make an empty MatchExpr match).
		return false
	}

	// String-list predicates
	if len(e.NameIn) > 0 && !PredNameIn(e.NameIn, t) {
		return false
	}
	if len(e.NameHasPrefix) > 0 && !PredNameHasPrefix(e.NameHasPrefix, t) {
		return false
	}
	if len(e.HasBodyText) > 0 && !PredHasBodyText(e.HasBodyText, t, pf) {
		return false
	}

	// Nested struct predicates
	if e.ParamNameMatches != nil && !PredParamNameMatches(*e.ParamNameMatches, t) {
		return false
	}
	if e.CallWithoutKwarg != nil && !PredCallWithoutKwarg(*e.CallWithoutKwarg, t, pf) {
		return false
	}
	if e.CallWithKwargValue != nil && !PredCallWithKwargValue(*e.CallWithKwargValue, t, pf) {
		return false
	}
	if e.CallUsesUnnormalizedPathParam != nil && !PredCallUsesUnnormalizedPathParam(*e.CallUsesUnnormalizedPathParam, t, pf) {
		return false
	}
	if e.ToolDecoratorKwargValue != nil && !PredToolDecoratorKwargValue(*e.ToolDecoratorKwargValue, t) {
		return false
	}
	if len(e.ToolDecoratorKwargPresent) > 0 && !PredToolDecoratorKwargPresent(e.ToolDecoratorKwargPresent, t) {
		return false
	}

	return true
}

// predicatesByScope maps each scope to the predicate names its Evaluate* method
// dispatches. This MUST mirror EvaluateTool / EvaluateAgent / EvaluateSubagent /
// EvaluateRepo above: a predicate evaluated there but missing here would be
// wrongly rejected at load time, and a predicate listed here but not evaluated
// would be a silent no-op. The combinators (all/any/not) and `always` are
// scope-agnostic and intentionally absent.
var predicatesByScope = map[models.Scope]map[string]bool{
	models.ScopeTool: {
		"has_docstring": true, "has_params": true, "has_typed_params": true,
		"has_raise": true, "has_try_except": true, "has_shell_call": true,
		"has_code_exec_call": true,
		"has_write_call": true, "has_dynamic_url_call": true,
		"name_in": true, "name_has_prefix": true, "has_body_text": true,
		"param_name_matches": true, "call_without_kwarg": true,
		"call_with_kwarg_value": true, "call_uses_unnormalized_path_param": true,
		"tool_decorator_kwarg_value": true, "tool_decorator_kwarg_present": true,
	},
	models.ScopeAgent: {
		"agent_class": true, "agent_kwarg_present": true, "agent_kwarg_missing": true,
		"agent_kwarg_list_empty": true, "agent_kwarg_value": true,
		"agent_uses_tool_kind": true, "agent_grants_builtin_tool": true,
		"agent_handoff_to_class": true, "agent_uses_hosted_tool_class": true,
		"agent_is_subagent_of_any": true,
	},
	models.ScopeSubagent: {
		"subagent_grants_tool": true,
	},
	models.ScopeRepo: {
		"repo_has_sdk_dep": true, "repo_has_sdk_in_code": true,
		"repo_has_agent_class": true, "repo_has_no_agent_class": true,
		"repo_component_present": true, "repo_uses_default_tracing": true,
	},
}

// setPredicateNames returns the YAML names of every predicate field set on THIS
// node — excluding the scope-agnostic combinators (all/any/not) and `always`.
// It does not recurse; callers walk combinators. Order is field-declaration
// order, so output is deterministic.
func (e MatchExpr) setPredicateNames() []string {
	var n []string
	add := func(set bool, name string) {
		if set {
			n = append(n, name)
		}
	}
	// Tool scope
	add(e.HasDocstring != nil, "has_docstring")
	add(e.HasParams != nil, "has_params")
	add(e.HasTypedParams != nil, "has_typed_params")
	add(e.HasRaise != nil, "has_raise")
	add(e.HasTryExcept != nil, "has_try_except")
	add(e.HasShellCall != nil, "has_shell_call")
	add(e.HasCodeExecCall != nil, "has_code_exec_call")
	add(e.HasWriteCall != nil, "has_write_call")
	add(e.HasDynamicURLCall != nil, "has_dynamic_url_call")
	add(len(e.NameIn) > 0, "name_in")
	add(len(e.NameHasPrefix) > 0, "name_has_prefix")
	add(len(e.HasBodyText) > 0, "has_body_text")
	add(e.ParamNameMatches != nil, "param_name_matches")
	add(e.CallWithoutKwarg != nil, "call_without_kwarg")
	add(e.CallWithKwargValue != nil, "call_with_kwarg_value")
	add(e.CallUsesUnnormalizedPathParam != nil, "call_uses_unnormalized_path_param")
	add(e.ToolDecoratorKwargValue != nil, "tool_decorator_kwarg_value")
	add(len(e.ToolDecoratorKwargPresent) > 0, "tool_decorator_kwarg_present")
	// Agent scope
	add(len(e.AgentClass) > 0, "agent_class")
	add(len(e.AgentKwargPresent) > 0, "agent_kwarg_present")
	add(len(e.AgentKwargMissing) > 0, "agent_kwarg_missing")
	add(len(e.AgentKwargListEmpty) > 0, "agent_kwarg_list_empty")
	add(e.AgentKwargValue != nil, "agent_kwarg_value")
	add(len(e.AgentUsesToolKind) > 0, "agent_uses_tool_kind")
	add(len(e.AgentGrantsBuiltinTool) > 0, "agent_grants_builtin_tool")
	add(len(e.AgentHandoffToClass) > 0, "agent_handoff_to_class")
	add(len(e.AgentUsesHostedToolClass) > 0, "agent_uses_hosted_tool_class")
	add(e.AgentIsSubagentOfAny != nil, "agent_is_subagent_of_any")
	// Subagent scope
	add(len(e.SubagentGrantsTool) > 0, "subagent_grants_tool")
	// Repo scope
	add(len(e.RepoHasSDKDep) > 0, "repo_has_sdk_dep")
	add(len(e.RepoHasSDKInCode) > 0, "repo_has_sdk_in_code")
	add(len(e.RepoHasAgentClass) > 0, "repo_has_agent_class")
	add(len(e.RepoHasNoAgentClass) > 0, "repo_has_no_agent_class")
	add(len(e.RepoComponentPresent) > 0, "repo_component_present")
	add(e.RepoUsesDefaultTracing != nil, "repo_uses_default_tracing")
	return n
}

// outOfScopePredicates returns the YAML names of predicates set anywhere in the
// match tree (recursing through all/any/not) that the given scope's evaluator
// does not dispatch. A non-empty result means the rule would silently fire more
// broadly than authored, since the out-of-scope clauses are dropped at
// evaluation. Order is deterministic (field order; combinators walked
// all→any→not).
func (e MatchExpr) outOfScopePredicates(scope models.Scope) []string {
	allowed := predicatesByScope[scope]
	var bad []string
	for _, name := range e.setPredicateNames() {
		if !allowed[name] {
			bad = append(bad, name)
		}
	}
	for _, sub := range e.All {
		bad = append(bad, sub.outOfScopePredicates(scope)...)
	}
	for _, sub := range e.Any {
		bad = append(bad, sub.outOfScopePredicates(scope)...)
	}
	if e.Not != nil {
		bad = append(bad, e.Not.outOfScopePredicates(scope)...)
	}
	return bad
}

// isEmpty reports whether the expression sets no combinator and no predicate
// (including `always`). An empty top-level match is a valid singleton (matches
// vacuously, gated only by applies_to); an empty expression *inside* a `not:`
// is a degenerate footgun (see degenerateCombinators).
func (e MatchExpr) isEmpty() bool {
	return e.All == nil && e.Any == nil && e.Not == nil &&
		e.Always == nil && len(e.setPredicateNames()) == 0
}

// degenerateCombinators returns descriptions of meaningless combinators in the
// match tree: an empty `all:`/`any:` list (a present-but-empty sequence, not an
// absent one) and a `not:` wrapping an empty expression. `any: []` vacuously
// passes today — matching everything, the opposite of "at least one of these" —
// and `not: {}` is always false; both are authoring mistakes, so the loader
// rejects them rather than silently mis-evaluating. Recurses through all/any/not.
func (e MatchExpr) degenerateCombinators() []string {
	var bad []string
	if e.All != nil && len(e.All) == 0 {
		bad = append(bad, "empty all")
	}
	if e.Any != nil && len(e.Any) == 0 {
		bad = append(bad, "empty any")
	}
	if e.Not != nil && e.Not.isEmpty() {
		bad = append(bad, "not over an empty expression")
	}
	for _, sub := range e.All {
		bad = append(bad, sub.degenerateCombinators()...)
	}
	for _, sub := range e.Any {
		bad = append(bad, sub.degenerateCombinators()...)
	}
	if e.Not != nil {
		bad = append(bad, e.Not.degenerateCombinators()...)
	}
	return bad
}
