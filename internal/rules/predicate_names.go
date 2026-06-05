package rules

import (
	"reflect"
	"strings"
)

// combinatorsAndAlways are the scope-agnostic MatchExpr fields that are not
// predicates: the all/any/not combinators and the `always` sentinel. They are
// valid keys inside a match tree but are handled structurally, not as
// predicates, so they are excluded from the predicate-name set.
var combinatorsAndAlways = map[string]bool{
	"all": true, "any": true, "not": true, "always": true,
}

// KnownPredicateKeys returns the set of YAML predicate keys this engine build
// understands — every MatchExpr field tag except the combinators and `always`.
//
// It is the single source of truth for two consumers: the schema/scope drift
// tests (which assert every predicate is documented and scoped), and the
// lenient loader's forward-compatibility check. A rule whose match references a
// key outside this set ∪ the combinators comes from a newer rule schema this
// build cannot evaluate; the lenient runtime loader skips such a rule rather
// than hard-failing the whole pack (see loadPolicies / rulesWithUnknownMatchKeys).
func KnownPredicateKeys() map[string]bool {
	names := map[string]bool{}
	rt := reflect.TypeOf(MatchExpr{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" || combinatorsAndAlways[name] {
			continue
		}
		names[name] = true
	}
	return names
}

// knownMatchKeys returns the full set of keys the lenient loader recognizes
// inside a match tree: every known predicate plus the combinator/always keys.
// A match key outside this set marks its rule as forward-incompatible.
func knownMatchKeys() map[string]bool {
	m := KnownPredicateKeys()
	for k := range combinatorsAndAlways {
		m[k] = true
	}
	return m
}
