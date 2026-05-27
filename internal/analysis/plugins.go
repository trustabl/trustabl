package analysis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

type pluginManifestJSON struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"plugins"`
}

// DiscoverPlugins parses .claude-plugin/plugin.json and marketplace.json files.
// Unlike DiscoverSlashCommands (which reads pre-tagged Components), this filters
// manifest.JSONFiles directly by path + basename. A file with a non-empty
// plugins[] array is a marketplace; otherwise it is a plain plugin. Malformed
// JSON is skipped silently. Line/EndLine are not tracked for JSON manifests
// (Line=1, EndLine=line count).
func DiscoverPlugins(manifest models.ScanManifest) []models.PluginManifest {
	var out []models.PluginManifest
	for _, p := range manifest.JSONFiles {
		if !hasPluginSegment(p) {
			continue
		}
		base := filepath.Base(p)
		if base != "plugin.json" && base != "marketplace.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manifest.RepoRoot, p))
		if err != nil {
			continue
		}
		var parsed pluginManifestJSON
		if err := json.Unmarshal(raw, &parsed); err != nil {
			continue
		}
		kind := "plugin"
		if len(parsed.Plugins) > 0 {
			kind = "marketplace"
		}
		entries := make([]models.PluginEntry, 0, len(parsed.Plugins))
		for _, e := range parsed.Plugins {
			entries = append(entries, models.PluginEntry{Name: e.Name, Source: e.Source})
		}
		out = append(out, models.PluginManifest{
			Kind:     kind,
			Name:     parsed.Name,
			Plugins:  entries,
			Location: models.Location{FilePath: p, Line: 1, EndLine: strings.Count(string(raw), "\n") + 1},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}

// hasPluginSegment reports whether forward-slash path p is under a
// .claude-plugin/ directory at any depth. Manifest paths are forward-slash
// normalized by the ingestion layer.
func hasPluginSegment(p string) bool {
	return strings.HasPrefix(p, ".claude-plugin/") || strings.Contains(p, "/.claude-plugin/")
}
