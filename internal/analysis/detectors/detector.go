// Package detectors defines the Detector interface and the Registry that
// runs them. Concrete detectors live elsewhere — today the only producer is
// internal/rules, which loads YAML policies and wraps each one as a
// RuleDetector that satisfies this interface.
//
// Discipline:
//   - A detector is pure: same inputs → same findings. No I/O, no clocks.
//   - A detector returns 0 or more Findings. A finding is one diagnosable issue.
//   - Each finding MUST set Confidence and a human Explanation per architecture §7
//     ("show your work"). Trust in generated configs depends on this.
package detectors

import (
	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/models"
)

// Detector is the unit of analysis. One instance per rule.
type Detector interface {
	// RuleID returns a stable identifier like "CSDK-001" or "OSH-002".
	RuleID() string
	// Category is the AutoFix category this detector feeds.
	Category() models.DetectorCategory
	// Applies returns true if this detector should run against the given tool.
	Applies(tool models.ToolDef) bool
	// Detect runs the rule. The ParsedFile is supplied so the detector can
	// re-walk the AST around the tool's function — handy for body-level checks.
	Detect(tool models.ToolDef, pf analysis.ParsedFile) []models.Finding
	// Singleton returns true if this detector should fire at most once per scan
	// (against the first applicable tool). Use for manifest-level checks that
	// don't vary per tool.
	Singleton() bool
}

// Registry is the set of detectors active for a scan.
type Registry struct {
	detectors []Detector
}

// New returns a Registry holding the given detectors. The order is preserved
// across Run() so output is deterministic when the input slice is.
func New(ds []Detector) *Registry {
	return &Registry{detectors: ds}
}

// Subset returns a new registry containing only detectors in the given categories.
func (r *Registry) Subset(cats ...models.DetectorCategory) *Registry {
	want := make(map[models.DetectorCategory]struct{}, len(cats))
	for _, c := range cats {
		want[c] = struct{}{}
	}
	out := &Registry{}
	for _, d := range r.detectors {
		if _, ok := want[d.Category()]; ok {
			out.detectors = append(out.detectors, d)
		}
	}
	return out
}

// Run executes every applicable detector against every tool, returning all
// findings. Order is detector-stable then tool-stable so output is
// reproducible. Singleton detectors fire at most once per scan.
func (r *Registry) Run(tools []models.ToolDef, files []analysis.ParsedFile) []models.Finding {
	byPath := map[string]analysis.ParsedFile{}
	for _, f := range files {
		byPath[f.RelPath] = f
	}
	fired := map[string]bool{} // tracks singletons that have already fired
	var out []models.Finding
	for _, d := range r.detectors {
		for _, t := range tools {
			if d.Singleton() && fired[d.RuleID()] {
				continue
			}
			if !d.Applies(t) {
				continue
			}
			pf, ok := byPath[t.FilePath]
			if !ok {
				continue
			}
			findings := d.Detect(t, pf)
			if d.Singleton() && len(findings) > 0 {
				fired[d.RuleID()] = true
			}
			out = append(out, findings...)
		}
	}
	return out
}

// Count returns the number of registered detectors. Useful for reporting.
func (r *Registry) Count() int { return len(r.detectors) }
