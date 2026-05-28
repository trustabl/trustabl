package scanner

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestScanID_FoldsAllFileLists guards the §7 honesty half of the determinism
// contract: "different inputs → different ScanID". The ID must change when ANY
// inventoried source/config file list changes, not just PythonFiles — the
// engine now does first-class TypeScript/JavaScript discovery and markdown /
// JSON / YAML config scanning, so an ID derived from PythonFiles alone collides
// across materially different scans of a non-Python repo.
func TestScanID_FoldsAllFileLists(t *testing.T) {
	base := models.ScanManifest{PythonFiles: []string{"main.py"}}
	baseID := scanID("repo", base, "v1")

	cases := []struct {
		name   string
		mutate func(*models.ScanManifest)
	}{
		{"TypeScriptFiles", func(m *models.ScanManifest) { m.TypeScriptFiles = []string{"src/tool.ts"} }},
		{"JavaScriptFiles", func(m *models.ScanManifest) { m.JavaScriptFiles = []string{"src/tool.js"} }},
		{"MarkdownFiles", func(m *models.ScanManifest) { m.MarkdownFiles = []string{".claude/agents/x.md"} }},
		{"JSONFiles", func(m *models.ScanManifest) { m.JSONFiles = []string{".claude/settings.json"} }},
		{"YAMLFiles", func(m *models.ScanManifest) { m.YAMLFiles = []string{"openshell/policy.yaml"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)
			if got := scanID("repo", m, "v1"); got == baseID {
				t.Errorf("ScanID unchanged when %s differs: both %q", tc.name, got)
			}
		})
	}
}

// TestScanID_StableUnderReordering keeps the determinism half intact: a given
// file list in a different OS-walk order must still produce the same ID.
func TestScanID_StableUnderReordering(t *testing.T) {
	a := models.ScanManifest{
		PythonFiles:     []string{"a.py", "b.py"},
		TypeScriptFiles: []string{"x.ts", "y.ts"},
	}
	b := models.ScanManifest{
		PythonFiles:     []string{"b.py", "a.py"},
		TypeScriptFiles: []string{"y.ts", "x.ts"},
	}
	if scanID("repo", a, "v1") != scanID("repo", b, "v1") {
		t.Error("ScanID changed under file reordering; must be order-independent")
	}
}
