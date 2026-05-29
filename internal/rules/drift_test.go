package rules

import (
	"reflect"
	"strings"
	"testing"
)

// The match layer keeps three hand-maintained tables that MUST agree on the set
// of predicate names: the MatchExpr struct fields (the anchor / source of
// truth), predicatesByScope (used by the loader to reject out-of-scope
// predicates), and setPredicateNames (used to walk which predicates a node
// sets). evaluator.go's own comment flags the drift risk: a predicate missing
// from predicatesByScope is wrongly rejected at load, and one listed there but
// never dispatched is a silent no-op. These tests anchor both maps to the
// struct so adding a predicate field without updating both fails the build.
//
// Not covered here: whether each Evaluate* method actually dispatches the
// predicate (a "listed but not evaluated" no-op). That is caught indirectly by
// the per-rule coverage guard in policies_test.go — any predicate used by a
// real rule with a fire case must dispatch or the fire case fails.

// combinatorAndAlways are the scope-agnostic MatchExpr fields that are
// intentionally absent from both predicatesByScope and setPredicateNames.
var combinatorAndAlways = map[string]bool{
	"all": true, "any": true, "not": true, "always": true,
}

// reflectedPredicateNames returns the YAML name of every MatchExpr field that
// is a predicate (i.e. not a combinator or `always`).
func reflectedPredicateNames(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	rt := reflect.TypeOf(MatchExpr{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" || combinatorAndAlways[name] {
			continue
		}
		names[name] = true
	}
	return names
}

func TestPredicatesByScope_MirrorsStruct(t *testing.T) {
	want := reflectedPredicateNames(t)
	got := map[string]bool{}
	for scope, preds := range predicatesByScope {
		for name := range preds {
			if got[name] {
				t.Errorf("predicate %q appears in more than one scope (must be scoped to exactly one)", name)
			}
			got[name] = true
			_ = scope
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("predicate %q is a MatchExpr field but missing from predicatesByScope — it would be wrongly rejected at load time", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("predicate %q is in predicatesByScope but is not a MatchExpr field — stale entry", name)
		}
	}
}

func TestSetPredicateNames_MirrorsStruct(t *testing.T) {
	want := reflectedPredicateNames(t)

	// Populate every predicate field with a non-zero value so setPredicateNames
	// reports it (it checks `!= nil` for pointers and `len > 0` for slices).
	var e MatchExpr
	rv := reflect.ValueOf(&e).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" || combinatorAndAlways[name] {
			continue
		}
		f := rv.Field(i)
		switch f.Kind() {
		case reflect.Ptr:
			f.Set(reflect.New(f.Type().Elem()))
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		default:
			t.Fatalf("predicate field %q has unhandled kind %s; extend this drift test", name, f.Kind())
		}
	}

	got := map[string]bool{}
	for _, n := range e.setPredicateNames() {
		got[n] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("predicate %q is a MatchExpr field but setPredicateNames does not emit it — it would be invisible to scope validation", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("setPredicateNames emits %q which is not a MatchExpr predicate field — stale entry", name)
		}
	}
}
