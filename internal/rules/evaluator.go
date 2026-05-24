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
