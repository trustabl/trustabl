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
	SchemaVersion int                 `json:"schema_version"`
	ForwardCompat bool                `json:"forward_compat"`
	Scopes        []string            `json:"scopes"`
	Languages     []string            `json:"languages"`
	Categories    []string            `json:"categories"`
	AppliesTo     map[string][]string `json:"applies_to"`
	Predicates    []string            `json:"predicates"`
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
		// This build is forward-compatible: LoadLenient skips a rule whose scope,
		// applies_to, language, or predicate it does not understand rather than
		// hard-failing the pack. Releases predating that behavior (e.g. v0.1.3)
		// have a hand-authored descriptor with forward_compat=false, which tells
		// the CI gate that an out-of-vocabulary rule CRASHES them, not skips.
		ForwardCompat: true,
		Scopes:        scopes,
		Languages:     langs,
		Categories:    cats,
		AppliesTo:     appliesTo,
		Predicates:    preds,
	}
}
