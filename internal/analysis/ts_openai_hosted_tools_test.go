package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
)

func TestIsTSOpenAIHostedToolFactory(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"webSearchTool", true},
		{"fileSearchTool", true},
		{"codeInterpreterTool", true},
		{"imageGenerationTool", true},
		{"toolSearchTool", true},
		{"computerTool", true},
		{"shellTool", true},
		{"applyPatchTool", true},
		{"hostedMcpTool", true},
		{"WebSearchTool", false}, // PascalCase = Python; must not match
		{"customTool", false},
		{"tool", false},
		{"", false},
	}
	for _, c := range cases {
		if got := analysis.IsTSOpenAIHostedToolFactory(c.name); got != c.want {
			t.Errorf("IsTSOpenAIHostedToolFactory(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTSOpenAIHostedToolFactories_NineFactories(t *testing.T) {
	if got := len(analysis.TSOpenAIHostedToolFactories); got != 9 {
		t.Errorf("expected 9 factories, got %d: %v", got, analysis.TSOpenAIHostedToolFactories)
	}
}
