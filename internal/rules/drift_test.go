package rules

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaYAML_DocumentsEveryPredicate closes the gap the package CLAUDE.md
// implies is guarded: schema.yaml is the human reference for the schema, but
// nothing previously asserted it stays in sync with the MatchExpr struct (the
// source of truth). A predicate added to schema.go but never documented in
// schema.yaml would let that reference silently rot. Membership-only check (the
// name appears somewhere in the file) — enough to flag an undocumented
// predicate without over-constraining the prose layout.
func TestSchemaYAML_DocumentsEveryPredicate(t *testing.T) {
	doc, err := os.ReadFile("schema.yaml")
	if err != nil {
		t.Fatalf("read schema.yaml: %v", err)
	}
	text := string(doc)
	for name := range reflectedPredicateNames(t) {
		if !strings.Contains(text, name) {
			t.Errorf("predicate %q is a MatchExpr field but is not documented anywhere in schema.yaml — the human schema reference has drifted from schema.go", name)
		}
	}
}

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
