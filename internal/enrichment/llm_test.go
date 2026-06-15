package enrichment

import (
	"context"
	"strings"
	"testing"
)

func TestSalvagePartialJSON_Complete(t *testing.T) {
	raw := `[{"explanation":"e1","fix":"f1","line_start":5,"line_end":7,"replacement":"new code","false_positive":false}]`
	got := salvagePartialJSON(raw, 1)
	if got[0].Explanation != "e1" {
		t.Errorf("Explanation = %q, want e1", got[0].Explanation)
	}
	if got[0].LineStart != 5 {
		t.Errorf("LineStart = %d, want 5", got[0].LineStart)
	}
}

func TestSalvagePartialJSON_Truncated(t *testing.T) {
	// Second object is incomplete — first should be recovered
	raw := `[{"explanation":"e1","fix":"f1","line_start":5,"line_end":7,"replacement":"x","false_positive":false},{"explanation":"e2`
	got := salvagePartialJSON(raw, 2)
	if got[0].Explanation != "e1" {
		t.Errorf("Explanation[0] = %q, want e1", got[0].Explanation)
	}
	// Second object is zero-value (parse failed)
	if got[1].Explanation != "" {
		t.Errorf("Explanation[1] = %q, want empty (truncated)", got[1].Explanation)
	}
}

func TestSalvagePartialJSON_Empty(t *testing.T) {
	got := salvagePartialJSON("not json at all", 2)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (zero-value slots)", len(got))
	}
}

func TestStripFence_JSONFence(t *testing.T) {
	raw := "```json\n[{\"explanation\":\"e1\"}]\n```"
	stripped := stripFence(raw)
	if strings.Contains(stripped, "```") {
		t.Errorf("fence not stripped: %q", stripped)
	}
	if !strings.Contains(stripped, `"explanation"`) {
		t.Errorf("content lost after strip: %q", stripped)
	}
}

func TestStripFence_NoFence(t *testing.T) {
	raw := `[{"explanation":"e1"}]`
	got := stripFence(raw)
	if got != raw {
		t.Errorf("stripFence modified non-fenced input: got %q, want %q", got, raw)
	}
}

func TestIndentBlock(t *testing.T) {
	in := "line1\nline2\nline3"
	got := indentBlock(in, "   ")
	lines := strings.Split(got, "\n")
	for _, l := range lines {
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "   ") {
			t.Errorf("line %q not indented", l)
		}
	}
}

func TestNewLLMClient_UnknownProvider(t *testing.T) {
	_, err := newLLMClient(context.Background(), "unknown", "key", "model")
	if err == nil {
		t.Fatal("newLLMClient with unknown provider: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error %q should contain \"unsupported\"", err.Error())
	}
}

func TestBuildPrompt_ContainsKeyFields(t *testing.T) {
	issues := []issueContext{
		{
			ruleID:      "CSDK-010",
			title:       "Missing guardrail",
			severity:    "high",
			ruleScope:   "agent",
			line:        42,
			explanation: "No input_guardrails set",
			fixTemplate: "Add input_guardrails=[...]",
			codeBlock:   "→ agent = Agent(...)\n",
		},
	}
	prompt := buildIssueList(issues)
	for _, want := range []string{"CSDK-010", "Missing guardrail", "high", "agent", "42", "No input_guardrails"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
