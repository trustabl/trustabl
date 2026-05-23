package sarif

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestLevelForSeverity(t *testing.T) {
	cases := map[models.Severity]string{
		models.SeverityCritical: "error",
		models.SeverityHigh:     "error",
		models.SeverityMedium:   "warning",
		models.SeverityLow:      "note",
		models.SeverityInfo:     "note",
	}
	for sev, want := range cases {
		if got := levelForSeverity(sev); got != want {
			t.Errorf("levelForSeverity(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestSecuritySeverityForSeverity(t *testing.T) {
	cases := map[models.Severity]string{
		models.SeverityCritical: "9.0",
		models.SeverityHigh:     "7.5",
		models.SeverityMedium:   "5.5",
		models.SeverityLow:      "3.0",
		models.SeverityInfo:     "0.5",
	}
	for sev, want := range cases {
		if got := securitySeverityForSeverity(sev); got != want {
			t.Errorf("securitySeverityForSeverity(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestTagsForFinding(t *testing.T) {
	// Category, Scope (derived from RuleID), Language ("python" default).
	f := models.Finding{
		RuleID:   "OAI-101",
		Category: models.CategoryOpenAISDK,
	}
	tags := tagsForFinding(f)
	wantContains := []string{"openai_sdk", "python"}
	for _, w := range wantContains {
		found := false
		for _, tag := range tags {
			if tag == w {
				found = true
			}
		}
		if !found {
			t.Errorf("tagsForFinding missing %q in %v", w, tags)
		}
	}
}

func TestRuleFromFinding(t *testing.T) {
	f := models.Finding{
		RuleID:       "OAI-101",
		Category:     models.CategoryOpenAISDK,
		Severity:     models.SeverityHigh,
		Title:        "Agent with shell tools and no input_guardrails",
		Explanation:  "An agent that exposes shell-invoking tools without input_guardrails is unsafe.",
		SuggestedFix: "Add input_guardrails = [...] to the Agent(...) constructor.",
		Confidence:   0.85,
	}
	rd := ruleFromFinding(f)
	if rd.ID != "OAI-101" {
		t.Errorf("ID = %q, want OAI-101", rd.ID)
	}
	if rd.ShortDescription == nil || rd.ShortDescription.Text != f.Title {
		t.Errorf("ShortDescription = %v, want %q", rd.ShortDescription, f.Title)
	}
	if rd.FullDescription == nil || rd.FullDescription.Text != f.Explanation {
		t.Errorf("FullDescription = %v, want %q", rd.FullDescription, f.Explanation)
	}
	if rd.Help == nil || rd.Help.Text != f.SuggestedFix {
		t.Errorf("Help = %v, want %q", rd.Help, f.SuggestedFix)
	}
	if rd.DefaultConfiguration == nil || rd.DefaultConfiguration.Level != "error" {
		t.Errorf("DefaultConfiguration.Level = %v, want error", rd.DefaultConfiguration)
	}
	if rd.Properties["security-severity"] != "7.5" {
		t.Errorf("security-severity = %v, want 7.5", rd.Properties["security-severity"])
	}
	if rd.Properties["confidence"] != 0.85 {
		t.Errorf("confidence = %v, want 0.85", rd.Properties["confidence"])
	}
	tags, ok := rd.Properties["tags"].([]string)
	if !ok {
		t.Fatalf("tags missing or wrong type: %T", rd.Properties["tags"])
	}
	if len(tags) == 0 {
		t.Error("tags is empty")
	}
}
