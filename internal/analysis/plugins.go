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
		Name string `json:"name"`
		// Source is captured as RawMessage because marketplace.json entries
		// use either a string ("./local-path") OR an object
		// ({"source":"git-subdir","url":"…","path":"…"}). A typed string here
		// would fail json.Unmarshal on the object form and silently drop the
		// entire marketplace.
		Source json.RawMessage `json:"source"`
	} `json:"plugins"`
}

// normalizePluginSource collapses a marketplace plugins[].source value
// (RawMessage) into a human-readable string preserved on PluginEntry.Source.
// Recognized shapes:
//
//   - JSON string ("./local-foo" or "https://…") → returned as-is.
//   - Object with a known shape ({"source":"git-subdir","url":"…","path":"…"})
//     → formatted as "<source>:<url>#<path>" so downstream consumers can still
//     distinguish trust categories (local vs git-subdir vs git) at a glance.
//   - Anything else → raw JSON, so nothing is silently dropped.
//
// Returns "" when the raw value is empty.
func normalizePluginSource(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Source string `json:"source"`
		URL    string `json:"url"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Source != "" {
		out := obj.Source
		if obj.URL != "" {
			out += ":" + obj.URL
		}
		if obj.Path != "" {
			out += "#" + obj.Path
		}
		return out
	}
	return string(raw)
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
			entries = append(entries, models.PluginEntry{Name: e.Name, Source: normalizePluginSource(e.Source)})
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
