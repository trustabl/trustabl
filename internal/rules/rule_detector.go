package rules

import (
	"io/fs"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/analysis/detectors"
	"github.com/trustabl/karenctl/internal/models"
)

// RuleDetector adapts a YAML-loaded RuleDef into a detectors.Detector. One
// instance per rule. The adapter is the only place where the rules package
// touches the detectors package boundary.
type RuleDetector struct {
	rule RuleDef
}

// NewRuleDetector returns a Detector that fires the given rule.
func NewRuleDetector(rule RuleDef) detectors.Detector {
	return &RuleDetector{rule: rule}
}

// RuleID returns the rule's stable ID (e.g. "CSDK-001").
func (d *RuleDetector) RuleID() string { return d.rule.ID }

// Category returns the rule's detector category.
func (d *RuleDetector) Category() models.DetectorCategory { return d.rule.Category }

// Singleton returns whether this rule should fire at most once per scan.
func (d *RuleDetector) Singleton() bool { return d.rule.Singleton }

// Applies returns whether this rule should fire against the given tool.
// A rule applies when BOTH:
//   - the tool's Kind is listed in the rule's `applies_to`, and
//   - the tool's Language matches the rule's Language.
//
// The language gate is what makes per-language rule sets safe to coexist:
// a Python-targeted rule never tries to evaluate against a TypeScript tool.
func (d *RuleDetector) Applies(t models.ToolDef) bool {
	if d.rule.Language != t.Language {
		return false
	}
	for _, kind := range d.rule.AppliesTo {
		if string(t.Kind) == kind {
			return true
		}
	}
	return false
}

// Detect evaluates the rule's MatchExpr against the tool. Returns one
// Finding when the expression matches, nil otherwise.
//
// Line precision: the original hardcoded detectors sometimes reported a
// call-site line (e.g. CSDK-003 pointed at the requests.get line, not the
// function). Rule-driven detectors use t.Line — the function's start —
// uniformly. Regression accepted: the explanation text already names the
// kind of call, and CI consumers get the function's file/line for jump-to.
func (d *RuleDetector) Detect(t models.ToolDef, pf analysis.ParsedFile) []models.Finding {
	if !d.rule.Match.Evaluate(t, pf) {
		return nil
	}
	return []models.Finding{{
		RuleID:       d.rule.ID,
		Category:     d.rule.Category,
		Severity:     d.rule.Severity,
		ToolName:     t.Name,
		FilePath:     t.FilePath,
		Line:         t.Line,
		Title:        d.rule.Title,
		Explanation:  d.rule.Explanation,
		SuggestedFix: d.rule.Fix,
		Confidence:   d.rule.Confidence,
		FixHints:     d.rule.FixHints,
	}}
}

// LoadRegistry loads policies from fsys and returns a populated detector
// Registry. Use with rules.DefaultFS() for the embedded built-ins, or any
// fs.FS for tests / out-of-tree policy bundles.
func LoadRegistry(fsys fs.FS) (*detectors.Registry, error) {
	policies, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	var ds []detectors.Detector
	for _, p := range policies {
		for _, r := range p.Rules {
			ds = append(ds, NewRuleDetector(r))
		}
	}
	return detectors.New(ds), nil
}
