// Package detectors defines typed Detector interfaces and the Registry that
// runs them. Concrete detectors live elsewhere — today the only producer is
// internal/rules, which loads YAML policies and wraps each one as a typed
// rule detector.
//
// Discipline:
//   - A detector is pure: same inputs → same findings. No I/O, no clocks.
//   - A detector returns 0 or more Findings. A finding is one diagnosable issue.
//   - Each finding MUST set Confidence and a human Explanation per architecture §7
//     ("show your work"). Trust in generated configs depends on this.
package detectors

import (
	"sort"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// ToolDetector fires against one ToolDef at a time.
type ToolDetector interface {
	RuleID() string
	Category() models.DetectorCategory
	Applies(models.ToolDef) bool
	Detect(models.ToolDef, analysis.ParsedFile, models.RepoInventory) []models.Finding
}

// AgentDetector fires against one AgentDef at a time.
type AgentDetector interface {
	RuleID() string
	Category() models.DetectorCategory
	Applies(models.AgentDef) bool
	Detect(models.AgentDef, models.RepoInventory) []models.Finding
}

// RepoDetector fires once per scan against the manifest.
type RepoDetector interface {
	RuleID() string
	Category() models.DetectorCategory
	Applies(models.RepoProfile, models.RepoInventory) bool
	Detect(models.RepoProfile, models.RepoInventory) []models.Finding
}

// Registry is the set of detectors active for a scan.
type Registry struct {
	tool  []ToolDetector
	agent []AgentDetector
	repo  []RepoDetector
}

// New returns a Registry holding the given detectors.
func New(tool []ToolDetector, agent []AgentDetector, repo []RepoDetector) *Registry {
	return &Registry{tool: tool, agent: agent, repo: repo}
}

// Run executes every applicable detector across tools, agents, and repo,
// returning all findings sorted deterministically.
func (r *Registry) Run(profile models.RepoProfile, inv models.RepoInventory, parsed []analysis.ParsedFile) []models.Finding {
	var out []models.Finding
	for _, d := range r.tool {
		for _, t := range inv.Tools {
			if !d.Applies(t) {
				continue
			}
			pf := parsedFor(t.FilePath, parsed)
			out = append(out, d.Detect(t, pf, inv)...)
		}
	}
	for _, d := range r.agent {
		for _, a := range inv.Agents {
			if !d.Applies(a) {
				continue
			}
			out = append(out, d.Detect(a, inv)...)
		}
	}
	for _, d := range r.repo {
		if !d.Applies(profile, inv) {
			continue
		}
		out = append(out, d.Detect(profile, inv)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Subset returns a new registry containing only detectors in the given categories.
func (r *Registry) Subset(cats ...models.DetectorCategory) *Registry {
	cset := make(map[models.DetectorCategory]bool, len(cats))
	for _, c := range cats {
		cset[c] = true
	}
	var sub Registry
	for _, d := range r.tool {
		if cset[d.Category()] {
			sub.tool = append(sub.tool, d)
		}
	}
	for _, d := range r.agent {
		if cset[d.Category()] {
			sub.agent = append(sub.agent, d)
		}
	}
	for _, d := range r.repo {
		if cset[d.Category()] {
			sub.repo = append(sub.repo, d)
		}
	}
	return &sub
}

// Count returns the total number of registered detectors.
func (r *Registry) Count() int { return len(r.tool) + len(r.agent) + len(r.repo) }

func parsedFor(filePath string, parsed []analysis.ParsedFile) analysis.ParsedFile {
	for _, pf := range parsed {
		if pf.RelPath == filePath {
			return pf
		}
	}
	return analysis.ParsedFile{}
}
