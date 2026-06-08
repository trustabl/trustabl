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
	"fmt"
	"sort"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// safeDetect runs one detector's Detect call, recovering any panic so a single
// malformed rule cannot crash the whole scan. Rule packs are loaded from an
// external repo (trustabl-rules), so a rule that steers a predicate into a nil
// dereference or an out-of-range index is untrusted input — it must degrade to
// a skipped detector plus a diagnostic finding, not a process abort. The
// returned finding is emitted through the same deterministic sort as every other
// finding, so recovery does not perturb byte-stable output.
func safeDetect(ruleID string, cat models.DetectorCategory, detect func() []models.Finding) (findings []models.Finding) {
	defer func() {
		if r := recover(); r != nil {
			findings = []models.Finding{{
				RuleID:      ruleID,
				Category:    cat,
				Severity:    models.SeverityInfo,
				Title:       "Detector skipped after internal error",
				Explanation: fmt.Sprintf("Rule %s could not be evaluated and was skipped (internal error: %v). This is an engine or rule-pack defect, not a finding about the scanned code.", ruleID, r),
				Confidence:  1,
			}}
		}
	}()
	return detect()
}

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

// SubagentDetector fires against one SubagentDef at a time. Subagents are
// .claude/agents/*.md frontmatter declarations — no function body or AST, so
// Detect takes no ParsedFile (like AgentDetector).
type SubagentDetector interface {
	RuleID() string
	Category() models.DetectorCategory
	Applies(models.SubagentDef) bool
	Detect(models.SubagentDef, models.RepoInventory) []models.Finding
}

// SkillDetector fires against one SkillDef at a time. Skills are SKILL.md
// frontmatter + body declarations — no function AST — so Detect takes no
// ParsedFile (like SubagentDetector).
type SkillDetector interface {
	RuleID() string
	Category() models.DetectorCategory
	Applies(models.SkillDef) bool
	Detect(models.SkillDef, models.RepoInventory) []models.Finding
}

// Registry is the set of detectors active for a scan.
type Registry struct {
	tool     []ToolDetector
	agent    []AgentDetector
	repo     []RepoDetector
	subagent []SubagentDetector
	skill    []SkillDetector
}

// New returns a Registry holding the given detectors.
func New(tool []ToolDetector, agent []AgentDetector, repo []RepoDetector, subagent []SubagentDetector, skill []SkillDetector) *Registry {
	return &Registry{tool: tool, agent: agent, repo: repo, subagent: subagent, skill: skill}
}

// Run executes every applicable detector across tools, agents, and repo,
// returning all findings sorted deterministically.
// onEntity, if non-nil, is called once per tool ("tool: <name>") and once
// per agent ("agent: <name>") as the scan progresses. The final sort
// guarantees output is identical regardless of the iteration order here.
func (r *Registry) Run(profile models.RepoProfile, inv models.RepoInventory, parsed []analysis.ParsedFile, onEntity func(label string)) []models.Finding {
	var out []models.Finding
	// Entity-major: visit each tool once (ticking progress), running every
	// applicable tool detector. Output order is normalized by the sort below,
	// so this reordering does not change results.
	for _, t := range inv.Tools {
		if onEntity != nil {
			onEntity("tool: " + t.Name)
		}
		pf := parsedFor(t.FilePath, parsed)
		for _, d := range r.tool {
			if !d.Applies(t) {
				continue
			}
			out = append(out, safeDetect(d.RuleID(), d.Category(), func() []models.Finding { return d.Detect(t, pf, inv) })...)
		}
	}
	for _, a := range inv.Agents {
		if onEntity != nil {
			onEntity("agent: " + a.Name)
		}
		for _, d := range r.agent {
			if !d.Applies(a) {
				continue
			}
			out = append(out, safeDetect(d.RuleID(), d.Category(), func() []models.Finding { return d.Detect(a, inv) })...)
		}
	}
	for _, s := range inv.Subagents {
		if onEntity != nil {
			onEntity("subagent: " + s.Name)
		}
		for _, d := range r.subagent {
			if !d.Applies(s) {
				continue
			}
			out = append(out, safeDetect(d.RuleID(), d.Category(), func() []models.Finding { return d.Detect(s, inv) })...)
		}
	}
	for _, s := range inv.Skills {
		if onEntity != nil {
			onEntity("skill: " + s.Name)
		}
		for _, d := range r.skill {
			if !d.Applies(s) {
				continue
			}
			out = append(out, safeDetect(d.RuleID(), d.Category(), func() []models.Finding { return d.Detect(s, inv) })...)
		}
	}
	for _, d := range r.repo {
		if !d.Applies(profile, inv) {
			continue
		}
		out = append(out, safeDetect(d.RuleID(), d.Category(), func() []models.Finding { return d.Detect(profile, inv) })...)
	}
	// Sort on a total order, then dedup. (RuleID, FilePath, Line) alone is not a
	// total order — two findings from the same rule attributed to the same
	// file+line but a different surface (ToolName) or message (Title) would tie
	// and fall back to entity-iteration order, leaking that order into the
	// byte-stable report. ToolName then Title close the tie; an adjacent-dedup
	// pass then drops exact duplicates so the same hit reported twice (e.g. a tool
	// resolved from two agents) cannot bloat the report non-deterministically.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].ToolName != out[j].ToolName {
			return out[i].ToolName < out[j].ToolName
		}
		return out[i].Title < out[j].Title
	})
	deduped := out[:0]
	for i, f := range out {
		if i > 0 {
			p := out[i-1]
			if f.RuleID == p.RuleID && f.FilePath == p.FilePath && f.Line == p.Line &&
				f.ToolName == p.ToolName && f.Title == p.Title {
				continue
			}
		}
		deduped = append(deduped, f)
	}
	return deduped
}

// ApplicableCategories returns the set of detector categories that had at
// least one TOOL, AGENT, or SUBAGENT detector whose Applies() returned true for
// at least one discovered entity. This is "could a rule even run against this
// repo's code", distinct from "did a rule fire" — a clean repo still has
// applicable categories; a repo whose SDK was detected but yielded no
// analyzable tools/agents does not. It gates the coverage-gap META-004 finding.
//
// Repo-scope detectors are deliberately EXCLUDED. A repo-scope rule's Applies()
// is true for the whole SDK (it only checks scope + SDK-detected + language),
// so counting it would mark the category "audited" for every repo of that SDK —
// even a TypeScript-only repo whose tools/agents discovery cannot parse, which
// is exactly the unaudited case META-004 exists to surface. Repo rules audit
// repo-wide config (e.g. CSDK-201's .claude/settings.json defaultMode), not the
// tool/agent code, so they must not suppress the "your code is unaudited"
// signal. The `profile` parameter is retained for call-site symmetry.
func (r *Registry) ApplicableCategories(profile models.RepoProfile, inv models.RepoInventory) map[models.DetectorCategory]bool {
	out := make(map[models.DetectorCategory]bool)
	for _, d := range r.tool {
		for _, t := range inv.Tools {
			if d.Applies(t) {
				out[d.Category()] = true
				break
			}
		}
	}
	for _, d := range r.agent {
		for _, a := range inv.Agents {
			if d.Applies(a) {
				out[d.Category()] = true
				break
			}
		}
	}
	for _, d := range r.subagent {
		for _, s := range inv.Subagents {
			if d.Applies(s) {
				out[d.Category()] = true
				break
			}
		}
	}
	for _, d := range r.skill {
		for _, s := range inv.Skills {
			if d.Applies(s) {
				out[d.Category()] = true
				break
			}
		}
	}
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
	for _, d := range r.subagent {
		if cset[d.Category()] {
			sub.subagent = append(sub.subagent, d)
		}
	}
	for _, d := range r.skill {
		if cset[d.Category()] {
			sub.skill = append(sub.skill, d)
		}
	}
	return &sub
}

// Count returns the total number of registered detectors.
func (r *Registry) Count() int {
	return len(r.tool) + len(r.agent) + len(r.repo) + len(r.subagent) + len(r.skill)
}

func parsedFor(filePath string, parsed []analysis.ParsedFile) analysis.ParsedFile {
	for _, pf := range parsed {
		if pf.RelPath == filePath {
			return pf
		}
	}
	return analysis.ParsedFile{}
}
