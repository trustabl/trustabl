package scanner

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// defaultOrigin is the unsigned-default origin tag; the file-list/schema tests
// hold it constant so they isolate the input under test.
var defaultOrigin = models.RulesOrigin{}.Tag()

// TestScanID_FoldsAllFileLists guards the §7 honesty half of the determinism
// contract: "different inputs → different ScanID". The ID must change when ANY
// inventoried source/config file list changes, not just PythonFiles — the
// engine now does first-class TypeScript/JavaScript discovery and markdown /
// JSON / YAML config scanning, so an ID derived from PythonFiles alone collides
// across materially different scans of a non-Python repo.
func TestScanID_FoldsAllFileLists(t *testing.T) {
	base := models.ScanManifest{PythonFiles: []string{"main.py"}}
	baseID := scanID("repo", base, "v1", 8, defaultOrigin, "")

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
			if got := scanID("repo", m, "v1", 8, defaultOrigin, ""); got == baseID {
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
	if scanID("repo", a, "v1", 8, defaultOrigin, "") != scanID("repo", b, "v1", 8, defaultOrigin, "") {
		t.Error("ScanID changed under file reordering; must be order-independent")
	}
}

// TestScanID_FoldsEngineSchemaVersion guards the forward-compatibility honesty
// addition: two builds supporting different schema versions can skip different
// forward-incompatible rules from the same pack, so the engine's supported
// schema version is folded into the ID. Same inputs but a different engine
// schema → different ID.
func TestScanID_FoldsEngineSchemaVersion(t *testing.T) {
	m := models.ScanManifest{PythonFiles: []string{"main.py"}}
	if scanID("repo", m, "v1", 8, defaultOrigin, "") == scanID("repo", m, "v1", 9, defaultOrigin, "") {
		t.Error("ScanID unchanged when engine schema version differs")
	}
}

// TestScanID_FoldsRulesOrigin guards the ENG-5 honesty addition: a scan of the
// same code with rules of different provenance (signed production, a pre-release
// channel, an unsigned custom source, the unsigned default) must get a distinct
// ScanID, while the same origin reproduces the same ID.
func TestScanID_FoldsRulesOrigin(t *testing.T) {
	m := models.ScanManifest{PythonFiles: []string{"main.py"}}
	origins := map[string]string{
		"prod":    models.RulesOrigin{Signed: true, Channel: "production"}.Tag(),
		"staging": models.RulesOrigin{Signed: true, Channel: "staging"}.Tag(),
		"custom":  models.RulesOrigin{Custom: true}.Tag(),
		"default": models.RulesOrigin{}.Tag(),
	}

	seen := map[string]string{}
	for name, tag := range origins {
		id := scanID("repo", m, "v1", 9, tag, "")
		if other, dup := seen[id]; dup {
			t.Errorf("ScanID collision across origins: %s and %s both %q", name, other, id)
		}
		seen[id] = name
	}

	// Determinism half: the same origin reproduces the same ID.
	prod := origins["prod"]
	if scanID("repo", m, "v1", 9, prod, "") != scanID("repo", m, "v1", 9, prod, "") {
		t.Error("ScanID not reproducible for a fixed origin")
	}
}

// TestScanID_FoldsVulnDBVersionOnlyWhenSet guards the --vuln-scan ScanID
// contract: an empty vulnDBVersion (the default, non-vuln-scan path) must NOT
// change the ID — so default scans stay byte-identical — while a set snapshot
// version must, keeping the ID honest about which vuln data produced findings.
func TestScanID_FoldsVulnDBVersionOnlyWhenSet(t *testing.T) {
	m := models.ScanManifest{PythonFiles: []string{"main.py"}}
	base := scanID("repo", m, "v1", 9, defaultOrigin, "")
	if scanID("repo", m, "v1", 9, defaultOrigin, "") != base {
		t.Error("empty vulnDBVersion must reproduce the same ID (default path unchanged)")
	}
	if scanID("repo", m, "v1", 9, defaultOrigin, "osv-abc123") == base {
		t.Error("a set vulnDBVersion must change the ScanID")
	}
}
