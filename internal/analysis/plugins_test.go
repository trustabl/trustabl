package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func manifestWithJSON(dir string, paths ...string) models.ScanManifest {
	return models.ScanManifest{RepoRoot: dir, JSONFiles: paths}
}

func TestPlugins_ParsesMarketplace(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude-plugin/marketplace.json",
		`{"name":"voltagent-subagents","plugins":[{"name":"core","source":"./categories/01-core"},{"name":"lang","source":"./categories/02-lang"}]}`)
	got := analysis.DiscoverPlugins(manifestWithJSON(dir, ".claude-plugin/marketplace.json"))
	if len(got) != 1 {
		t.Fatalf("got %d manifests, want 1", len(got))
	}
	m := got[0]
	if m.Kind != "marketplace" || m.Name != "voltagent-subagents" {
		t.Errorf("kind/name = %q / %q", m.Kind, m.Name)
	}
	if len(m.Plugins) != 2 || m.Plugins[0].Source != "./categories/01-core" {
		t.Errorf("plugins = %+v", m.Plugins)
	}
}

func TestPlugins_ParsesPluginJSON(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude-plugin/plugin.json", `{"name":"my-plugin","version":"1.0.0"}`)
	got := analysis.DiscoverPlugins(manifestWithJSON(dir, ".claude-plugin/plugin.json"))
	if len(got) != 1 || got[0].Kind != "plugin" || got[0].Name != "my-plugin" {
		t.Fatalf("got %+v", got)
	}
}

func TestPlugins_MalformedSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude-plugin/plugin.json", `{not json`)
	if got := analysis.DiscoverPlugins(manifestWithJSON(dir, ".claude-plugin/plugin.json")); len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func TestPlugins_IgnoresNonPluginJSON(t *testing.T) {
	dir := t.TempDir()
	// Right basename but not under .claude-plugin/ -> ignored.
	writeFixture(t, dir, "config/plugin.json", `{"name":"x"}`)
	// Under .claude-plugin/ but unrelated basename -> ignored.
	writeFixture(t, dir, ".claude-plugin/other.json", `{"name":"y"}`)
	got := analysis.DiscoverPlugins(manifestWithJSON(dir, "config/plugin.json", ".claude-plugin/other.json"))
	if len(got) != 0 {
		t.Fatalf("got %+v, want 0", got)
	}
}

func TestPlugins_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "z/.claude-plugin/plugin.json", `{"name":"z"}`)
	writeFixture(t, dir, "a/.claude-plugin/plugin.json", `{"name":"a"}`)
	got := analysis.DiscoverPlugins(manifestWithJSON(dir, "z/.claude-plugin/plugin.json", "a/.claude-plugin/plugin.json"))
	if len(got) != 2 || got[0].FilePath > got[1].FilePath {
		t.Fatalf("expected sorted by FilePath, got %+v", got)
	}
}
