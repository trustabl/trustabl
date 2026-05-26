package analysis

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverSubagents reads every ComponentSubagent in the manifest, parses the
// YAML frontmatter between the leading `---` markers, and returns one
// SubagentDef per file with frontmatter. Files without frontmatter are
// skipped silently (a subagent without frontmatter is just markdown
// documentation — it has no name/tools to act on).
func DiscoverSubagents(manifest models.ScanManifest) []models.SubagentDef {
	var out []models.SubagentDef
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentSubagent {
			continue
		}
		full := filepath.Join(manifest.RepoRoot, c.Path)
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		fm, ok := extractFrontmatter(raw)
		if !ok {
			continue
		}
		var parsed subagentFrontmatter
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			continue
		}
		if parsed.Name == "" {
			continue
		}
		out = append(out, models.SubagentDef{
			Name:        parsed.Name,
			Description: parsed.Description,
			Model:       parsed.Model,
			Tools:       splitToolsField([]string(parsed.Tools)),
			Location:    models.Location{FilePath: c.Path},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}

// stringOrList unmarshals a YAML value that may be either a scalar string or
// a sequence of strings into a []string. Claude subagent frontmatter uses both
// forms for the `tools:` field in the wild.
type stringOrList []string

func (s *stringOrList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = stringOrList{value.Value}
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*s = stringOrList(list)
	return nil
}

type subagentFrontmatter struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Tools       stringOrList `yaml:"tools"`
	Model       string       `yaml:"model"`
}

// extractFrontmatter pulls the YAML block between leading "---\n" (or
// "---\r\n") and the next line beginning with "---". Returns (block, true) on
// success, (nil, false) if the file does not start with "---".
//
// Known v1 limitations: a line beginning with "---" inside the frontmatter
// body (e.g. a YAML document separator in a block scalar) truncates the block
// early; and on CRLF files a trailing "\r" is left on the last block line,
// which yaml.v3 tolerates.
func extractFrontmatter(raw []byte) ([]byte, bool) {
	hasLF := bytes.HasPrefix(raw, []byte("---\n"))
	hasCRLF := bytes.HasPrefix(raw, []byte("---\r\n"))
	if !hasLF && !hasCRLF {
		return nil, false
	}
	rest := raw[4:]
	if hasCRLF {
		rest = raw[5:]
	}
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, false
	}
	return rest[:end], true
}

// splitToolsField normalizes the frontmatter tools entries into a flat slice.
// Each entry is comma-split so the scalar form ("Read, Bash, Grep") expands;
// YAML-list entries (no comma) pass through unchanged. Returns nil when empty.
func splitToolsField(raw []string) []string {
	var out []string
	for _, entry := range raw {
		for _, p := range strings.Split(entry, ",") {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}
