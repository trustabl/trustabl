package analysis

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// writeManifest writes content at root/rel (creating parents) and returns rel,
// the repo-relative slash path used as a BundledFile.Path.
func writeManifest(t *testing.T, root, rel, content string) string {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func skillWith(name string, paths ...string) models.SkillDef {
	bf := make([]models.BundledFile, 0, len(paths))
	for _, p := range paths {
		bf = append(bf, models.BundledFile{Path: p, Kind: "resource"})
	}
	return models.SkillDef{Name: name, BundledFiles: bf}
}

func TestDiscoverSkillDependencies_Requirements(t *testing.T) {
	root := t.TempDir()
	rel := writeManifest(t, root, "skills/pip/requirements.txt", strings.Join([]string{
		"# a comment",
		"",
		"requests==2.31.0",
		"flask>=1.0,<2.0",
		"numpy",
		"pyyaml==6.0  # inline comment",
		"package[extra]==1.2.3",
		"django ; python_version < '3.8'",
		"-r other.txt",          // option line — skipped
		"-e .",                  // editable — skipped
		"git+https://x/y.git",   // VCS — skipped
		"./local-pkg",           // local path — skipped
		"https://example/p.whl", // URL — skipped
	}, "\n"))

	got := DiscoverSkillDependencies([]models.SkillDef{skillWith("pip", rel)}, root)
	want := []models.DepRef{
		{Name: "django", Version: "", Ecosystem: "pypi", Source: rel},
		{Name: "flask", Version: ">=1.0,<2.0", Ecosystem: "pypi", Source: rel},
		{Name: "numpy", Version: "", Ecosystem: "pypi", Source: rel},
		{Name: "package", Version: "1.2.3", Ecosystem: "pypi", Source: rel},
		{Name: "pyyaml", Version: "6.0", Ecosystem: "pypi", Source: rel},
		{Name: "requests", Version: "2.31.0", Ecosystem: "pypi", Source: rel},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requirements parse mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDiscoverSkillDependencies_PackageJSON_DedupAndScoped(t *testing.T) {
	root := t.TempDir()
	rel := writeManifest(t, root, "skills/npm/package.json", `{
	  "name": "demo",
	  "dependencies": { "lodash": "^4.17.21", "@types/node": "20.1.0" },
	  "devDependencies": { "lodash": "^4.17.21", "jest": "29.0.0" }
	}`)

	got := DiscoverSkillDependencies([]models.SkillDef{skillWith("npm", rel)}, root)
	want := []models.DepRef{
		{Name: "@types/node", Version: "20.1.0", Ecosystem: "npm", Source: rel},
		{Name: "jest", Version: "29.0.0", Ecosystem: "npm", Source: rel},
		{Name: "lodash", Version: "^4.17.21", Ecosystem: "npm", Source: rel}, // deduped across deps+devDeps
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("package.json parse mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDiscoverSkillDependencies_OrderingAndAttribution(t *testing.T) {
	root := t.TempDir()
	// Two skills, each with a pip manifest naming the same dep. The same
	// (name,version) from different files stays as two entries, attributed by
	// Source — and npm sorts before pypi regardless of discovery order.
	pip := writeManifest(t, root, "skills/a/requirements.txt", "requests==2.31.0\n")
	npm := writeManifest(t, root, "skills/b/package.json", `{"dependencies":{"requests":"2.31.0"}}`)

	got := DiscoverSkillDependencies([]models.SkillDef{skillWith("b", npm), skillWith("a", pip)}, root)
	want := []models.DepRef{
		{Name: "requests", Version: "2.31.0", Ecosystem: "npm", Source: npm},
		{Name: "requests", Version: "2.31.0", Ecosystem: "pypi", Source: pip},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordering/attribution mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestDiscoverSkillDependencies_NoManifests(t *testing.T) {
	root := t.TempDir()
	rel := writeManifest(t, root, "skills/x/helper.py", "print('hi')\n")
	if got := DiscoverSkillDependencies([]models.SkillDef{skillWith("x", rel)}, root); got != nil {
		t.Errorf("skill with no manifest should yield nil, got %+v", got)
	}
}
