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

// DiscoverSubagents returns one SubagentDef per discovered subagent file.
// It runs two passes:
//
//  1. Canonical pass — every ComponentSubagent in manifest.Components is
//     treated as a confirmed subagent regardless of frontmatter shape (a
//     .claude/agents/ file with no tools/model inherits the parent's grants).
//
//  2. Shape fallback — each path in manifest.MarkdownFiles that was not already
//     emitted in pass 1 is tested against a tight shape gate
//     (name + tools|model) to catch flat collections such as
//     categories/<NN>/*.md (VoltAgent layout). Generic docs with frontmatter
//     but no subagent fields are silently excluded.
//
// Dedup is by relative path; the final slice is sorted by FilePath.
func DiscoverSubagents(manifest models.ScanManifest) []models.SubagentDef {
	var out []models.SubagentDef
	seen := make(map[string]bool) // relative paths already emitted

	// Pass 1: canonical .claude/agents/ files tagged by the normalizer. A
	// canonical path is owned by this pass — mark it seen before parsing so a
	// file that fails to parse here is never re-read by the pass-2 fallback.
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentSubagent || seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		if def, ok := parseSubagentFile(manifest.RepoRoot, c.Path); ok {
			out = append(out, def)
		}
	}

	// Pass 2: shape fallback for flat collections (e.g. categories/*.md). A
	// markdown file with subagent-shaped frontmatter (name + tools|model) that
	// is not a SKILL.md and not under .claude/commands/ is treated as a subagent.
	for _, p := range manifest.MarkdownFiles {
		if seen[p] || !isSubagentCandidatePath(p) {
			continue
		}
		if def, ok := parseSubagentFile(manifest.RepoRoot, p); ok && subagentShapeOK(def) {
			out = append(out, def)
			seen[p] = true
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}

// parseSubagentFile reads one markdown file and parses its frontmatter into a
// SubagentDef. ok is false if the file is missing, has no frontmatter, has
// malformed YAML, or has no name.
func parseSubagentFile(repoRoot, relPath string) (models.SubagentDef, bool) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, relPath))
	if err != nil {
		return models.SubagentDef{}, false
	}
	fm, startLine, endLine, ok := extractFrontmatter(raw)
	if !ok {
		return models.SubagentDef{}, false
	}
	var parsed subagentFrontmatter
	if err := yaml.Unmarshal(fm, &parsed); err != nil {
		return models.SubagentDef{}, false
	}
	if parsed.Name == "" {
		return models.SubagentDef{}, false
	}
	tokens := splitToolsTokens([]string(parsed.Tools))
	return models.SubagentDef{
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
			FilePath: relPath,
			Line:     startLine,
			EndLine:  endLine,
		},
	}, true
}

// isSubagentCandidatePath excludes paths that belong to other artifact kinds so
// the shape fallback does not double-claim a skill or slash command.
func isSubagentCandidatePath(p string) bool {
	if filepath.Base(p) == "SKILL.md" {
		return false
	}
	if strings.HasPrefix(p, ".claude/commands/") || strings.Contains(p, "/.claude/commands/") {
		return false
	}
	return true
}

// subagentShapeOK is the tight false-positive gate for the flat-collection
// fallback: require a name AND at least one of tools/model. name+description
// alone matches too many generic docs.
func subagentShapeOK(d models.SubagentDef) bool {
	return d.Name != "" && (len(d.Tools) > 0 || d.Model != "")
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
