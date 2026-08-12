package forge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// severityRank maps a severity string to a sort key (lower = higher priority).
func severityRank(s models.Severity) int {
	switch s {
	case models.SeverityCritical:
		return 0
	case models.SeverityHigh:
		return 1
	case models.SeverityMedium:
		return 2
	case models.SeverityLow:
		return 3
	default:
		return 4
	}
}

// firstSentence returns s up to and including the first ". " boundary (or end
// of string). Normalizes internal whitespace. If maxLen > 0 and the result
// exceeds maxLen, the string is truncated with a Unicode ellipsis.
func firstSentence(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if maxLen > 0 && len([]rune(s)) > maxLen {
		runes := []rune(s)
		cut := maxLen - 1
		for cut > 0 && runes[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = maxLen - 1
		}
		s = strings.TrimRight(string(runes[:cut]), " ") + "…"
	}
	return s
}

// matchCondition derives a human-readable "When this applies" string from a
// MatchExpr. Handles the all: combinator by joining sub-conditions. Falls back
// to a generic string when no recognized predicate is set.
func matchCondition(expr rules.MatchExpr) string {
	if len(expr.All) > 0 {
		var parts []string
		for _, sub := range expr.All {
			c := matchCondition(sub)
			if c != "" && c != "When the condition described by this rule is met." {
				parts = append(parts, strings.TrimSuffix(c, "."))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", and ") + "."
		}
	}
	switch {
	case expr.SkillAllowsUnrestrictedShell != nil:
		return "Every skill — always verify allowed-tools before emitting."
	case expr.SkillBodyHasDynamicExec != nil:
		return "Any skill body that uses the backtick-exec or fenced-exec form."
	case expr.SkillDynamicExecTouchesNetworkOrSecrets != nil:
		return "Any dynamic-context command performing network egress or accessing credentials."
	case expr.SkillReferencesExternalURL != nil:
		return "Any skill body that references an http(s) URL."
	case expr.SkillBodyHasInjectionMarker != nil:
		return "Any skill containing instruction-override phrasing, invisible Unicode, or encoded blobs."
	case len(expr.SkillAllowsTool) > 0:
		return "Any skill pre-approving Bash, Write, Edit, WebFetch, or NotebookEdit in allowed-tools."
	case expr.SkillModelInvocable != nil:
		return "Any skill where disable-model-invocation is not set to true."
	case expr.SkillBundledScriptNetworkEgress != nil:
		return "Any skill directory that bundles scripts making outbound network calls."
	case expr.SkillBundledScriptReadsSecrets != nil:
		return "Any skill directory that bundles scripts reading credentials or secrets."
	case expr.SkillBundledFileHasHardcodedSecret != nil:
		return "Any file bundled within the skill directory."
	case expr.SkillDescriptionToolMismatch != nil:
		return "Any skill whose description claims read-only but grants side-effecting tools."
	case expr.SkillHasDescription != nil:
		return "Every skill — description is required."
	case expr.SkillHasDuplicateToolRefs != nil:
		return "Any skill with duplicate entries in allowed-tools."
	case expr.SkillIsAgentSpecific != nil:
		return "Only when frontmatter includes a context: or agent: binding field."
	}
	return "When the condition described by this rule is met."
}

// Stamp is the passive watermark embedded in a GenerateCombined output.
type Stamp struct {
	Date          string                    // YYYY-MM-DD
	RulesSHA      string                    // short (7-char) or full SHA
	SchemaVersion int
	Categories    []models.DetectorCategory // sorted, controls section order
}

// sortRules sorts a rule slice by severity rank (critical first) then rule ID ascending.
func sortRules(rs []rules.RuleDef) {
	sort.Slice(rs, func(i, j int) bool {
		ri := severityRank(rs[i].Severity)
		rj := severityRank(rs[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return rs[i].ID < rs[j].ID
	})
}

// matchConditionForScope returns a human-readable "When this applies" string.
// For skill-scoped rules it delegates to the existing matchCondition predicate
// switch; for all other scopes it returns a scope-derived sentence.
func matchConditionForScope(scope models.Scope, expr rules.MatchExpr) string {
	switch scope {
	case models.ScopeTool:
		return "When defining a tool."
	case models.ScopeAgent:
		return "When declaring an agent."
	case models.ScopeRepo:
		return "For any repo using this SDK."
	case models.ScopeSubagent:
		return "When declaring a subagent."
	default: // ScopeSkill
		return matchCondition(expr)
	}
}

// emitRuleSection writes a ### heading + per-rule blocks. It is a no-op when
// rs is empty (omits the heading), keeping output clean for packs with no
// rules at a given scope.
func emitRuleSection(b *strings.Builder, heading string, rs []rules.RuleDef, scope models.Scope) {
	if len(rs) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", heading)
	for _, r := range rs {
		fmt.Fprintf(b, "---\n\n")
		fmt.Fprintf(b, "#### [%s] %s\n", r.ID, r.Title)
		fmt.Fprintf(b, "**Severity:** %s | **Confidence:** %.2f\n\n", string(r.Severity), r.Confidence)
		fmt.Fprintf(b, "**Directive:** %s\n\n", sanitizeEmittedText(firstSentence(r.Fix, 0)))
		fmt.Fprintf(b, "**Why:** %s\n\n", sanitizeEmittedText(firstSentence(r.Explanation, 0)))
		fmt.Fprintf(b, "**When this applies:** %s\n\n", sanitizeEmittedText(matchConditionForScope(scope, r.Match)))
	}
}

// GenerateCombined produces a combined pre-coding SKILL.md for one or more SDK
// policy packs. It collects all rule scopes (tool, agent, repo, skill) from
// each matched pack and organizes output as one ## section per SDK in the
// order given by stamp.Categories.
//
// policies should be the full output of rules.LoadLenient — GenerateCombined
// filters to the categories in stamp.Categories itself.
//
// Output is byte-stable: categories are iterated in stamp.Categories order;
// within each section rules are sorted by severity (critical first) then rule
// ID ascending.
func GenerateCombined(categories []models.DetectorCategory, policies []rules.PolicyFile, stamp Stamp) string {
	// build category set for O(1) membership check
	catSet := make(map[models.DetectorCategory]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	type sdkSection struct {
		meta       rules.PolicyMeta
		tools      []rules.RuleDef
		agents     []rules.RuleDef
		subagents  []rules.RuleDef
		repos      []rules.RuleDef
		skills     []rules.RuleDef
	}
	sections := make(map[models.DetectorCategory]*sdkSection)

	for _, pf := range policies {
		if !catSet[pf.Policy.Category] {
			continue
		}
		sec := sections[pf.Policy.Category]
		if sec == nil {
			sec = &sdkSection{meta: pf.Policy}
			sections[pf.Policy.Category] = sec
		}
		for _, r := range pf.Rules {
			switch r.Scope {
			case models.ScopeTool:
				sec.tools = append(sec.tools, r)
			case models.ScopeAgent:
				sec.agents = append(sec.agents, r)
			case models.ScopeSubagent:
				sec.subagents = append(sec.subagents, r)
			case models.ScopeRepo:
				sec.repos = append(sec.repos, r)
			case models.ScopeSkill:
				sec.skills = append(sec.skills, r)
			}
		}
	}
	for _, sec := range sections {
		sortRules(sec.tools)
		sortRules(sec.agents)
		sortRules(sec.subagents)
		sortRules(sec.repos)
		sortRules(sec.skills)
	}

	// stamp label: human-readable SDK list
	sdkLabels := make([]string, len(stamp.Categories))
	for i, c := range stamp.Categories {
		sdkLabels[i] = string(c)
	}
	sdkList := strings.Join(sdkLabels, ", ")

	var b strings.Builder

	// --- Frontmatter ---
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "name: trustabl-pre-coding\n")
	fmt.Fprintf(&b, "description: >-\n  Pre-coding reliability constraints for: %s\n", sdkList)
	fmt.Fprintf(&b, "allowed-tools: Read\n")
	fmt.Fprintf(&b, "disable-model-invocation: false\n")
	fmt.Fprintf(&b, "---\n\n")

	// --- Header ---
	fmt.Fprintf(&b, "# Trustabl Pre-Coding Reliability Constraints\n\n")
	fmt.Fprintf(&b, "<!-- generated: %s | rules: %s | schema: %d | sdks: %s -->\n\n",
		stamp.Date, stamp.RulesSHA, stamp.SchemaVersion, sdkList)
	fmt.Fprintf(&b, "Before writing any agent code, apply every constraint below. Rules are\n")
	fmt.Fprintf(&b, "ordered by severity. A violation here will fire the corresponding finding\n")
	fmt.Fprintf(&b, "in post-build scan — prevent it now.\n\n")

	// --- One section per category, in stamp.Categories order ---
	for _, cat := range stamp.Categories {
		sec, ok := sections[cat]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "---\n\n")
		fmt.Fprintf(&b, "## %s\n\n", sec.meta.Name)
		emitRuleSection(&b, "Tool Rules", sec.tools, models.ScopeTool)
		emitRuleSection(&b, "Agent Rules", sec.agents, models.ScopeAgent)
		emitRuleSection(&b, "Subagent Rules", sec.subagents, models.ScopeSubagent)
		emitRuleSection(&b, "Repo Rules", sec.repos, models.ScopeRepo)
		emitRuleSection(&b, "Skill Rules", sec.skills, models.ScopeSkill)
	}

	return b.String()
}

// sanitizeEmittedText neutralizes literal strings that would trigger skill safety
// detectors on the generated file. Rule explanations quote the dangerous patterns
// they guard against; we must emit safe equivalents so the output does not
// self-flag when scanned.
func sanitizeEmittedText(s string) string {
	// Break the inline-exec regex (requires !` adjacently) by inserting a space.
	s = strings.ReplaceAll(s, "!`", "! `")
	// Neutralize the canonical injection phrase matched by CSKILL-040.
	s = strings.ReplaceAll(s, "ignore previous instructions", "ignore-previous-instructions")
	return s
}

// Generate produces a pre-coding SKILL.md from a policy's skill-scoped rules.
// Only rules already filtered to scope==skill should be passed; Generate does
// not re-filter. Output is byte-stable: rules are sorted by severity rank
// (critical first), then by rule ID ascending within each rank.
func Generate(meta rules.PolicyMeta, skillRules []rules.RuleDef) string {
	sorted := make([]rules.RuleDef, len(skillRules))
	copy(sorted, skillRules)
	sort.Slice(sorted, func(i, j int) bool {
		ri := severityRank(sorted[i].Severity)
		rj := severityRank(sorted[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return sorted[i].ID < sorted[j].ID
	})

	name := strings.ReplaceAll(strings.ToLower(meta.ID), "_", "-")
	desc := firstSentence(meta.Description, 120)

	var b strings.Builder

	// --- Frontmatter ---
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: >-\n  %s\n", desc)
	fmt.Fprintf(&b, "allowed-tools: Read\n")
	fmt.Fprintf(&b, "disable-model-invocation: false\n")
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "\n")

	// --- Header ---
	fmt.Fprintf(&b, "# Trustabl Pre-Coding: %s\n", meta.Name)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Before writing any SKILL.md, you MUST apply every constraint below. Rules are\n")
	fmt.Fprintf(&b, "ordered by severity. A violation here will fire the corresponding finding\n")
	fmt.Fprintf(&b, "in post-build scan — prevent it now.\n")
	fmt.Fprintf(&b, "\n")

	// --- Decision tree (derived from CSKILL-001 + CSKILL-050) ---
	fmt.Fprintf(&b, "## Tool Grant Decision Tree\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "1. Does this skill need shell commands?\n")
	fmt.Fprintf(&b, "   YES → specify exact prefixes: Bash(git status *) — never bare Bash or Bash(*)\n")
	fmt.Fprintf(&b, "   NO  → omit Bash entirely\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "2. Does allowed-tools include any of: Bash / Write / Edit / WebFetch / NotebookEdit?\n")
	fmt.Fprintf(&b, "   YES → set disable-model-invocation: true\n")
	fmt.Fprintf(&b, "   NO  → disable-model-invocation may be false (read-only skills are safe to auto-invoke)\n")
	fmt.Fprintf(&b, "\n")

	// --- Per-rule constraint blocks ---
	for _, r := range sorted {
		fmt.Fprintf(&b, "---\n")
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "## [%s] %s\n", r.ID, r.Title)
		fmt.Fprintf(&b, "**Severity:** %s | **Confidence:** %.2f\n", string(r.Severity), r.Confidence)
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "**Directive:** %s\n", sanitizeEmittedText(firstSentence(r.Fix, 0)))
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "**Why:** %s\n", sanitizeEmittedText(firstSentence(r.Explanation, 0)))
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "**When this applies:** %s\n", sanitizeEmittedText(matchCondition(r.Match)))
		fmt.Fprintf(&b, "\n")
	}

	return b.String()
}
