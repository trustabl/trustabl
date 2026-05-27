package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
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

func TestNormalize_NestedClaudeAgentsClassified(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("agent/.claude/agents/inbox-searcher.md", "---\nname: inbox-searcher\n---\n")
	mustWrite(".claude/agents/root-agent.md", "---\nname: root-agent\n---\n")
	mustWrite("agent/.claude/agents/notes.txt", "not a subagent")
	mustWrite("agent/.claude/settings.json", "{}")
	mustWrite("agent/.claude/commands/foo.md", "# cmd")

	src := &Source{RootPath: dir}
	m, err := Normalize(src)
	if err != nil {
		t.Fatal(err)
	}

	kindsByPath := map[string]models.ComponentKind{}
	for _, c := range m.Components {
		kindsByPath[c.Path] = c.Kind
	}
	want := map[string]models.ComponentKind{
		"agent/.claude/agents/inbox-searcher.md": models.ComponentSubagent,
		".claude/agents/root-agent.md":           models.ComponentSubagent,
		"agent/.claude/settings.json":            models.ComponentClaudeSettings,
		"agent/.claude/commands/foo.md":          models.ComponentSlashCommand,
	}
	for path, wantKind := range want {
		if got := kindsByPath[path]; got != wantKind {
			t.Errorf("path %q: got kind %q, want %q", path, got, wantKind)
		}
	}
	if k, found := kindsByPath["agent/.claude/agents/notes.txt"]; found {
		t.Errorf("notes.txt should not be classified, got kind %q", k)
	}
}

func TestNormalize_SkillAndPluginClassified(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".claude/skills/deploy/SKILL.md", "---\nname: deploy\n---\n")
	mustWrite(".claude-plugin/marketplace.json", `{"name":"m","plugins":[]}`)
	mustWrite(".claude-plugin/plugin.json", `{"name":"p"}`)

	src := &Source{RootPath: dir}
	m, err := Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	kindsByPath := map[string]models.ComponentKind{}
	for _, c := range m.Components {
		kindsByPath[c.Path] = c.Kind
	}
	want := map[string]models.ComponentKind{
		".claude/skills/deploy/SKILL.md":   models.ComponentSkill,
		".claude-plugin/marketplace.json":  models.ComponentPluginManifest,
		".claude-plugin/plugin.json":       models.ComponentPluginManifest,
	}
	for path, wantKind := range want {
		if got := kindsByPath[path]; got != wantKind {
			t.Errorf("path %q: got kind %q, want %q", path, got, wantKind)
		}
	}
}

// TestNormalize_PluginSlashCommandsClassified verifies the path-gate for
// slash commands is broad enough to catch plugin layouts (e.g.
// wshobson/agents stores commands at plugins/<x>/commands/*.md, not
// .claude/commands/). A markdown file under <root>/commands/ where <root>
// has a sibling .claude-plugin/plugin.json must be tagged as a slash command.
// A commands/ directory NOT adjacent to a plugin manifest must NOT be tagged.
func TestNormalize_PluginSlashCommandsClassified(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Plugin root with a real manifest — commands beneath it are slash commands.
	mustWrite("plugins/foo/.claude-plugin/plugin.json", `{"name":"foo"}`)
	mustWrite("plugins/foo/commands/run.md", "Run the thing.\n")
	mustWrite("plugins/foo/commands/check.md", "---\ndescription: Check\n---\n")
	// Sibling .md not in commands/ — must NOT be tagged.
	mustWrite("plugins/foo/README.md", "# Foo\n")
	// A commands/ dir without a sibling plugin manifest — must NOT be tagged.
	mustWrite("docs/commands/notes.md", "# Just docs\n")
	// Existing canonical path must still work (regression guard).
	mustWrite(".claude/commands/legacy.md", "Legacy\n")

	src := &Source{RootPath: dir}
	m, err := Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]models.ComponentKind{}
	for _, c := range m.Components {
		kinds[c.Path] = c.Kind
	}

	want := map[string]models.ComponentKind{
		"plugins/foo/commands/run.md":   models.ComponentSlashCommand,
		"plugins/foo/commands/check.md": models.ComponentSlashCommand,
		".claude/commands/legacy.md":    models.ComponentSlashCommand,
	}
	for path, wantKind := range want {
		if got := kinds[path]; got != wantKind {
			t.Errorf("path %q: got kind %q, want %q", path, got, wantKind)
		}
	}
	for _, p := range []string{"plugins/foo/README.md", "docs/commands/notes.md"} {
		if k := kinds[p]; k == models.ComponentSlashCommand {
			t.Errorf("path %q should not be a slash_command, got %q", p, k)
		}
	}
}
