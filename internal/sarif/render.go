package sarif

import (
	"github.com/trustabl/trustabl/internal/models"
)

// levelForSeverity maps Trustabl's 5-bucket severity to SARIF's 4-bucket level.
// Mapping locked in design doc D3. critical/high → error; medium → warning;
// low/info → note.
func levelForSeverity(s models.Severity) string {
	switch s {
	case models.SeverityCritical, models.SeverityHigh:
		return "error"
	case models.SeverityMedium:
		return "warning"
	default: // low, info, unknown
		return "note"
	}
}

// securitySeverityForSeverity maps to the GitHub "security-severity" 0–10
// string used to drive the Critical/High/Medium/Low badge bucketing. Cutoffs
// in the GitHub UI: ≥9 critical, ≥7 high, ≥4 medium, <4 low. Mapping locked
// in design doc D3.
func securitySeverityForSeverity(s models.Severity) string {
	switch s {
	case models.SeverityCritical:
		return "9.0"
	case models.SeverityHigh:
		return "7.5"
	case models.SeverityMedium:
		return "5.5"
	case models.SeverityLow:
		return "3.0"
	default: // info, unknown
		return "0.5"
	}
}

// tagsForFinding builds the rule-descriptor tag set: category, scope (parsed
// from the rule ID prefix when applicable), and language. Language defaults
// to "python" because Trustabl's discovery is python-only today and the
// loader fills "" with "python".
func tagsForFinding(f models.Finding) []string {
	tags := []string{}
	if f.Category != "" {
		tags = append(tags, string(f.Category))
	}
	// Trustabl's rule IDs encode scope implicitly:
	//   CSDK-0xx / OAI-0xx → tool scope
	//   CSDK-1xx / OAI-1xx → agent scope
	//   OAI-2xx           → repo scope
	//   META-xxx          → no scope tag
	if scope := scopeFromRuleID(f.RuleID); scope != "" {
		tags = append(tags, scope)
	}
	tags = append(tags, "python") // language; revisit when multi-language discovery lands
	return tags
}

// scopeFromRuleID returns the rule's scope tag based on its numeric prefix, or
// "" for META rules (no scope).
func scopeFromRuleID(id string) string {
	// id format: "<PREFIX>-<NNN>" where PREFIX is CSDK/OAI/META.
	// Scope buckets: 0xx tool, 1xx agent, 2xx repo.
	dash := -1
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			dash = i
			break
		}
	}
	if dash < 0 || dash+1 >= len(id) {
		return ""
	}
	prefix := id[:dash]
	if prefix == "META" {
		return ""
	}
	first := id[dash+1]
	switch first {
	case '0':
		return "tool"
	case '1':
		return "agent"
	case '2':
		return "repo"
	}
	return ""
}

// ruleFromFinding builds a SARIF reportingDescriptor (rule catalog entry) from
// the first Finding emitted for a given rule. Title/Explanation/Fix are
// rule-stable; severity is rule-stable; confidence is rule-stable today (may
// vary per-finding once value-aware predicates land — the descriptor will then
// reflect the rule's default and result.rank carries the per-finding value).
func ruleFromFinding(f models.Finding) ReportingDescriptor {
	return ReportingDescriptor{
		ID:               f.RuleID,
		ShortDescription: &Message{Text: f.Title},
		FullDescription:  &Message{Text: f.Explanation},
		Help:             &Message{Text: f.SuggestedFix},
		DefaultConfiguration: &ReportingConfiguration{
			Level: levelForSeverity(f.Severity),
		},
		Properties: map[string]any{
			"security-severity": securitySeverityForSeverity(f.Severity),
			"confidence":        f.Confidence,
			"tags":              tagsForFinding(f),
		},
	}
}
