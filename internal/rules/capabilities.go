package rules

import (
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

// Capabilities is the machine-readable vocabulary this engine build can evaluate:
// the rule-schema version plus every scope, language, detector category,
// applies_to value, and match predicate it understands, and whether it loads
// rules forward-compatibly (skips what it cannot evaluate) or hard-fails.
//
// It is the contract a rule pack is checked against. `trustabl capabilities
// --json` emits it; a release publishes its descriptor as an asset; and the
// trustabl-rules CI gate uses each supported release's descriptor to decide,
// before a rule change merges, whether a proposed rule would run, be skipped
// (forward-compatible), or hard-break that release — so a rules change can never
// silently break a deployed binary.
type Capabilities struct {
	SchemaVersion int      `json:"schema_version"`
	Scopes        []string `json:"scopes"`
	Languages     []string `json:"languages"`
	Categories    []string `json:"categories"`
	// HardFailDimensions names the vocabulary dimensions for which an
	// out-of-vocabulary value causes this build to HARD-FAIL the whole rule load
	// instead of skipping the offending rule. A fully forward-compatible build
	// (LoadLenient skips unknown scope/applies_to/language/category/predicate)
	// reports an empty list. The released v0.1.3 has a hand-authored descriptor
	// listing ["language"] — it skips the other dimensions but crashes on an
	// unknown language, which is exactly the incident this whole gate prevents.
	// The CI gate fails a rules PR if any rule uses an out-of-vocab value in a
	// hard-fail dimension of any supported release.
	HardFailDimensions []string            `json:"hard_fail_dimensions"`
	AppliesTo          map[string][]string `json:"applies_to"`
	Predicates         []string            `json:"predicates"`
}

// Describe builds the capability descriptor for this build from the same
// source-of-truth lists the loader validates against (models.AllScopes /
// AllLanguages / AllCategories, AppliesToByScope, KnownPredicateKeys), so the
// descriptor cannot drift from what the engine actually accepts. Output is
// deterministic: every slice is stably ordered or sorted, and encoding/json
// sorts the AppliesTo map keys.
func Describe() Capabilities {
	scopes := make([]string, len(models.AllScopes))
	for i, s := range models.AllScopes {
		scopes[i] = string(s)
	}
	langs := make([]string, len(models.AllLanguages))
	for i, l := range models.AllLanguages {
		langs[i] = string(l)
	}
	cats := make([]string, len(models.AllCategories))
	for i, c := range models.AllCategories {
		cats[i] = string(c)
	}
	appliesTo := make(map[string][]string)
	for s, kinds := range AppliesToByScope() {
		appliesTo[string(s)] = kinds
	}
	preds := make([]string, 0, len(KnownPredicateKeys()))
	for k := range KnownPredicateKeys() {
		preds = append(preds, k)
	}
	sort.Strings(preds)

	return Capabilities{
		SchemaVersion: SupportedSchemaVersion,
		Scopes:        scopes,
		Languages:     langs,
		Categories:    cats,
		// This build is fully forward-compatible: LoadLenient skips any rule whose
		// scope, applies_to, language, category, or predicate it does not
		// understand rather than hard-failing the pack. So no dimension hard-fails
		// — empty list. (Authored as a non-nil empty slice so the JSON is [] not
		// null.) Releases predating full forward-compat carry a hand-authored
		// descriptor naming the dimensions they crash on, e.g. v0.1.3 → ["language"].
		HardFailDimensions: []string{},
		AppliesTo:          appliesTo,
		Predicates:         preds,
	}
}
