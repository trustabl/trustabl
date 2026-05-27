package analysis

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestIsADKHostedToolClass(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"BashTool", "BashTool", true},
		{"GoogleSearchTool", "GoogleSearchTool", true},
		{"VertexAiSearchTool", "VertexAiSearchTool", true},
		{"AgentTool", "AgentTool", true},
		{"LongRunningTool", "LongRunningTool", true},
		{"LangchainTool", "LangchainTool", true},
		{"CrewaiTool", "CrewaiTool", true},
		{"WebSearchTool (OpenAI, not ADK)", "WebSearchTool", false},
		{"BashTools (typo)", "BashTools", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := IsADKHostedToolClass(c.in)
			if got != c.want {
				t.Errorf("IsADKHostedToolClass(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestClassifyADKHostedToolCall(t *testing.T) {
	item := models.Expr{Kind: models.ExprCall, Text: "BashTool()", Line: 5, EndLine: 5}
	h, ok := classifyADKHostedToolCall(item, "main.py")
	if !ok {
		t.Fatal("classifyADKHostedToolCall: ok = false, want true")
	}
	if h.Class != "BashTool" {
		t.Errorf("Class: got %q, want BashTool", h.Class)
	}
	if h.SDK != models.SDKGoogleADK {
		t.Errorf("SDK: got %q, want google_adk", h.SDK)
	}
	if h.FilePath != "main.py" || h.Line != 5 {
		t.Errorf("attribution: got %s:%d, want main.py:5", h.FilePath, h.Line)
	}
	if h.EndLine != 5 {
		t.Errorf("EndLine: got %d, want 5", h.EndLine)
	}

	nonCall := models.Expr{Kind: models.ExprNameRef, Text: "BashTool"}
	if _, ok := classifyADKHostedToolCall(nonCall, "main.py"); ok {
		t.Error("non-call item: ok = true, want false")
	}
}
