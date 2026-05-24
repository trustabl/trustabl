package ingestion

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSDKDeps_GoogleADK(t *testing.T) {
	dir := t.TempDir()
	pyproject := `[project]
dependencies = ["google-adk>=0.1.0"]
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := detectSDKDeps(dir)
	var found bool
	for _, d := range deps {
		if d.Name == "google-adk" && d.Source == "pyproject.toml" {
			found = true
		}
	}
	if !found {
		t.Errorf("google-adk not in detected deps: %+v", deps)
	}
}

func TestDetectSDKDeps_TSClaudeSDKFromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkg := `{
  "name": "demo",
  "dependencies": {
    "@anthropic-ai/claude-agent-sdk": "^1.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := detectSDKDeps(dir)
	var found bool
	for _, d := range deps {
		if d.Name == "claude-agent-sdk" && d.Source == "package.json" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected claude-agent-sdk@package.json in deps, got %+v", deps)
	}
}

func TestDetectSDKDeps_TSNeedleScopedToPackageJSONOnly(t *testing.T) {
	dir := t.TempDir()
	// A package.json with the TS package in devDependencies (a common pattern
	// for test code). The TS needle should find it. Combined with the first test,
	// this ensures the needle is scoped correctly: it fires on package.json but
	// not on Python manifests.
	pkg := `{
  "name": "test-suite",
  "devDependencies": {
    "@anthropic-ai/claude-agent-sdk": "^1.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := detectSDKDeps(dir)
	var found bool
	for _, d := range deps {
		if d.Name == "claude-agent-sdk" && d.Source == "package.json" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected claude-agent-sdk@package.json even in devDependencies, got %+v", deps)
	}
}

// TestDetectSDKDeps_TSPackageInPackageJSONProducesExactlyOneEntry guards the
// substring-collision footgun. The Python needle pattern "claude-agent-sdk"
// is a literal substring of the TS package id "@anthropic-ai/claude-agent-sdk".
// The Python needle is correctly scoped to Python manifests, so a package.json
// declaring only the TS dep must produce EXACTLY ONE claude-agent-sdk SDKDep
// (Source=package.json from the TS needle). If a future maintainer adds
// "package.json" to the Python needle's Manifests list, this test fails
// because two cross-fired entries appear. Read the maintainer comment in
// detectSDKDeps before "fixing" this test.
func TestDetectSDKDeps_TSPackageInPackageJSONProducesExactlyOneEntry(t *testing.T) {
	dir := t.TempDir()
	pkg := `{
  "name": "demo",
  "dependencies": {
    "@anthropic-ai/claude-agent-sdk": "^1.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := detectSDKDeps(dir)
	var matches []string
	for _, d := range deps {
		if d.Name == "claude-agent-sdk" {
			matches = append(matches, d.Source)
		}
	}
	if len(matches) != 1 {
		t.Errorf("expected exactly 1 claude-agent-sdk entry, got %d (sources: %v) — likely substring cross-fire", len(matches), matches)
		return
	}
	if matches[0] != "package.json" {
		t.Errorf("expected Source=package.json (TS needle), got %q", matches[0])
	}
}

func TestNormalize_CollectsMTSAndCTS(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.ts", "b.tsx", "c.mts", "d.cts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.TypeScriptFiles) != 4 {
		t.Errorf("expected 4 TS files (.ts/.tsx/.mts/.cts), got %d: %v", len(m.TypeScriptFiles), m.TypeScriptFiles)
	}
}
