package analysis

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"

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
		fm, startLine, endLine, ok := extractFrontmatter(raw)
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
		tokens := splitToolsTokens([]string(parsed.Tools))
		out = append(out, models.SubagentDef{
			Name:            parsed.Name,
			Description:     parsed.Description,
			Tools:           tokens,
			ToolGrants:      parseToolGrants(tokens),
			DisallowedTools: splitToolsTokens([]string(parsed.DisallowedTools)),
			Model:           parsed.Model,
			PermissionMode:  parsed.PermissionMode,
			MCPServers:      splitToolsTokens([]string(parsed.MCPServers)),
			Skills:          splitToolsTokens([]string(parsed.Skills)),
			HasHooks:        parsed.Hooks.Kind == yaml.MappingNode && len(parsed.Hooks.Content) > 0,
			Isolation:       parsed.Isolation,
			Location: models.Location{
				FilePath: c.Path,
				Line:     startLine,
				EndLine:  endLine,
			},
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
	Name            string       `yaml:"name"`
	Description     string       `yaml:"description"`
	Tools           stringOrList `yaml:"tools"`
	DisallowedTools stringOrList `yaml:"disallowedTools"`
	Model           string       `yaml:"model"`
	PermissionMode  string       `yaml:"permissionMode"`
	MCPServers      stringOrList `yaml:"mcpServers"`
	Skills          stringOrList `yaml:"skills"`
	Hooks           yaml.Node    `yaml:"hooks"`
	Isolation       string       `yaml:"isolation"`
}

// extractFrontmatter pulls the YAML block between leading "---\n" (or
// "---\r\n") and the next line beginning with "---". On success it returns
// (block, startLine, endLine, true) where startLine is the line of the
// opening "---" marker (always 1) and endLine is the line of the closing
// "---" marker. Returns (nil, 0, 0, false) if the file does not start with
// "---".
//
// Known v1 limitations: a line beginning with "---" inside the frontmatter
// body (e.g. a YAML document separator in a block scalar) truncates the block
// early; and on CRLF files a trailing "\r" is left on the last block line,
// which yaml.v3 tolerates.
func extractFrontmatter(raw []byte) (block []byte, startLine, endLine int, ok bool) {
	hasLF := bytes.HasPrefix(raw, []byte("---\n"))
	hasCRLF := bytes.HasPrefix(raw, []byte("---\r\n"))
	if !hasLF && !hasCRLF {
		return nil, 0, 0, false
	}
	headerLen := 4
	if hasCRLF {
		headerLen = 5
	}
	rest := raw[headerLen:]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, 0, 0, false
	}
	// The closing "\n---" begins at byte offset (headerLen + end) within raw.
	// The newline at that index terminates the last frontmatter-body line;
	// the "---" marker sits on the next line. Counting newlines in raw up to
	// and INCLUDING that terminating newline gives the marker's 1-indexed
	// line number directly.
	markerNewlineOffset := headerLen + end
	endLine = bytes.Count(raw[:markerNewlineOffset+1], []byte{'\n'}) + 1
	return rest[:end], 1, endLine, true
}

// splitToolsTokens normalizes frontmatter list entries (scalar comma/space form
// or YAML-list form) into flat, verbatim tokens. Each entry is run through
// splitToolGrants so a parametered grant ("Agent(a, b)") is preserved as one
// token. Returns nil when empty.
func splitToolsTokens(raw []string) []string {
	var out []string
	for _, entry := range raw {
		out = append(out, splitToolGrants(entry)...)
	}
	return out
}
