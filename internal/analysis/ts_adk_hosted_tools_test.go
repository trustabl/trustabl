package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
)

func TestIsTSADKHostedToolClass(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// 13 JS classes — all should match
		{"AgentTool", true},
		{"ExitLoopTool", true},
		{"GoogleMapsGroundingTool", true},
		{"GoogleSearchTool", true},
		{"LoadArtifactsTool", true},
		{"LoadMemoryTool", true},
		{"LongRunningTool", true},
		{"PreloadMemoryTool", true},
		{"UrlContextTool", true},
		{"VertexAiSearchTool", true},
		{"VertexRagRetrievalTool", true},
		{"RunSkillInlineScriptTool", true},
		{"RunSkillScriptTool", true},
		// Python-only classes — must NOT match (those have no JS factory)
		{"BashTool", false},
		{"LangchainTool", false},
		{"CrewaiTool", false},
		{"LoadWebPage", false},
		{"DiscoveryEngineSearchTool", false},
		{"EnterpriseSearchTool", false},
		// User-defined class names — must not match
		{"MyCustomTool", false},
		{"", false},
	}
	for _, c := range cases {
		if got := analysis.IsTSADKHostedToolClass(c.name); got != c.want {
			t.Errorf("IsTSADKHostedToolClass(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTSADKHostedToolClasses_ThirteenClasses(t *testing.T) {
	// If this count drifts, also extend TestIsTSADKHostedToolClass's
	// table with the new class names. Both tests must stay in sync.
	if got := len(analysis.TSADKHostedToolClasses); got != 13 {
		t.Errorf("expected 13 ADK JS hosted-tool classes, got %d: %v",
			got, analysis.TSADKHostedToolClasses)
	}
}
