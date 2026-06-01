package sarif

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestLanguageTagForPath(t *testing.T) {
	cases := map[string]string{
		"agents/web.py":  "python",
		"src/agent.ts":   "typescript",
		"src/agent.tsx":  "typescript",
		"src/agent.mts":  "typescript",
		"src/agent.js":   "javascript",
		"src/agent.mjs":  "javascript",
		"pyproject.toml": "python",     // unknown extension → python default
		"":               "python",     // repo-scope finding with no file
		"DIR/Agent.TS":   "typescript", // case-insensitive
	}
	for path, want := range cases {
		if got := languageTagForPath(path); got != want {
			t.Errorf("languageTagForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestTagsForFinding_TypeScriptFindingTaggedTypeScript(t *testing.T) {
	f := models.Finding{
		RuleID:   "OAI-005",
		Category: models.CategoryOpenAISDK,
		FilePath: "src/agent.ts",
	}
	tags := tagsForFinding(f)
	var hasTS, hasPy bool
	for _, tg := range tags {
		switch tg {
		case "typescript":
			hasTS = true
		case "python":
			hasPy = true
		}
	}
	if !hasTS {
		t.Errorf("TS finding not tagged typescript: %v", tags)
	}
	if hasPy {
		t.Errorf("TS finding wrongly tagged python: %v", tags)
	}
}
