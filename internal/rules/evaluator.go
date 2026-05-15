package rules

import (
	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/models"
)

// Evaluate walks a MatchExpr and returns whether the tool matches it.
//
// Semantics:
//   - An empty MatchExpr returns true (vacuously matches; useful for
//     singleton rules with `always: true`-equivalent intent).
//   - Every set field on a node contributes one boolean to a conjunction
//     (logical AND): all combinators, all primitives, and all nested
//     struct predicates that are non-nil must hold for the node to match.
//   - The combinators `all` and `any` recurse; `not` negates.
//
// This conjunctive default makes simple rules (one predicate per node) read
// naturally as YAML, while combinators are available when needed.
func (e MatchExpr) Evaluate(t models.ToolDef, pf analysis.ParsedFile) bool {
	// Combinators
	if len(e.All) > 0 {
		for _, sub := range e.All {
			if !sub.Evaluate(t, pf) {
				return false
			}
		}
	}
	if len(e.Any) > 0 {
		matched := false
		for _, sub := range e.Any {
			if sub.Evaluate(t, pf) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if e.Not != nil && e.Not.Evaluate(t, pf) {
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
	if e.CallUsesParam != nil && !PredCallUsesParam(*e.CallUsesParam, t, pf) {
		return false
	}
	if e.CallUsesUnnormalizedPathParam != nil && !PredCallUsesUnnormalizedPathParam(*e.CallUsesUnnormalizedPathParam, t, pf) {
		return false
	}

	return true
}
