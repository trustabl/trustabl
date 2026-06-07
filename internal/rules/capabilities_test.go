package rules_test

import (
	"sort"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

func capContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestDescribe_MatchesEngineVocabulary locks the capability descriptor to what
// the loader actually enforces: every advertised value must pass the matching
// validator, and the predicate list must equal KnownPredicateKeys. This is what
// lets the trustabl-rules CI gate trust the descriptor — if Describe drifted from
// the real vocabulary, the gate's verdicts would be wrong.
func TestDescribe_MatchesEngineVocabulary(t *testing.T) {
	c := rules.Describe()

	if c.SchemaVersion != rules.SupportedSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", c.SchemaVersion, rules.SupportedSchemaVersion)
	}
	if !c.ForwardCompat {
		t.Error("ForwardCompat = false; this build has LoadLenient and must report true")
	}

	if len(c.Scopes) == 0 || len(c.Languages) == 0 || len(c.Categories) == 0 || len(c.Predicates) == 0 {
		t.Fatalf("descriptor has an empty dimension: %+v", c)
	}
	for _, s := range c.Scopes {
		if !models.ValidScope(models.Scope(s)) {
			t.Errorf("advertised scope %q is not ValidScope", s)
		}
	}
	for _, l := range c.Languages {
		if !models.ValidLanguage(models.Language(l)) {
			t.Errorf("advertised language %q is not ValidLanguage", l)
		}
	}
	for _, cat := range c.Categories {
		if !models.ValidCategory(models.DetectorCategory(cat)) {
			t.Errorf("advertised category %q is not ValidCategory", cat)
		}
	}
	for scope, kinds := range c.AppliesTo {
		if !models.ValidScope(models.Scope(scope)) {
			t.Errorf("AppliesTo has unknown scope key %q", scope)
		}
		if len(kinds) == 0 {
			t.Errorf("AppliesTo[%q] is empty", scope)
		}
	}

	known := rules.KnownPredicateKeys()
	if len(c.Predicates) != len(known) {
		t.Errorf("descriptor lists %d predicates, KnownPredicateKeys has %d", len(c.Predicates), len(known))
	}
	for _, p := range c.Predicates {
		if !known[p] {
			t.Errorf("advertised predicate %q is not a KnownPredicateKey", p)
		}
	}

	// Representative spot-checks (guard an accidental empty/partial dimension).
	if !capContains(c.Languages, "go") || !capContains(c.Languages, "python") {
		t.Errorf("Languages missing expected entries: %v", c.Languages)
	}
	if !capContains(c.Scopes, "tool") {
		t.Errorf("Scopes missing tool: %v", c.Scopes)
	}
	if !capContains(c.AppliesTo["tool"], "mcp_tool") {
		t.Errorf("AppliesTo[tool] missing mcp_tool: %v", c.AppliesTo["tool"])
	}
	if !capContains(c.Predicates, "has_docstring") {
		t.Errorf("Predicates missing has_docstring: %v", c.Predicates)
	}

	// Determinism: the predicate list is sorted (the rest are stably ordered, and
	// encoding/json sorts the AppliesTo map keys).
	if !sort.StringsAreSorted(c.Predicates) {
		t.Errorf("Predicates not sorted (non-deterministic output): %v", c.Predicates)
	}
}
