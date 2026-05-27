package analysis

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// permRuleRegex matches "Tool" or "Tool(pattern)". Tool is alphanumeric and
// starts uppercase. Pattern is everything up to the final ')'.
var permRuleRegex = regexp.MustCompile(`^([A-Z][A-Za-z]+)(?:\(([^)]*)\))?$`)

// mcpToolLiteral matches the form mcp__<server>__<tool> (no parens).
var mcpToolLiteral = regexp.MustCompile(`^mcp__(.+)$`)

// rawPresent reports whether a json.RawMessage corresponds to a JSON key that
// was present with a non-null value. json.Unmarshal of `"key": null` yields a
// non-nil RawMessage holding the literal bytes `null`, so a plain nil check is
// not enough to tell "key absent" / "key explicitly null" from "key present".
func rawPresent(r json.RawMessage) bool {
	return len(r) > 0 && string(r) != "null"
}

// ParsePermissionRule parses one entry from the allow/deny/ask lists into a
// typed PermissionRule. Unknown shapes still emit a PermissionRule with the
// Raw field set so detectors can surface them; Tool will be empty.
//
// Known limitations (Raw is always the ground truth for disambiguation):
//   - "Bash" and "Bash()" both parse to Tool="Bash", Pattern="". The empty-
//     parens form is not distinguished from the bare form; a consumer that
//     needs the distinction must read Raw.
//   - Tool names must be at least two characters ([A-Z][A-Za-z]+). Every
//     known Claude tool name satisfies this; a hypothetical one-char tool
//     would fall through as Tool="".
func ParsePermissionRule(raw string) models.PermissionRule {
	rule := models.PermissionRule{Raw: raw}
	if m := mcpToolLiteral.FindStringSubmatch(raw); m != nil {
		rule.Tool = "MCP"
		rule.Pattern = m[1]
		return rule
	}
	if m := permRuleRegex.FindStringSubmatch(raw); m != nil {
		rule.Tool = m[1]
		rule.Pattern = m[2]
	}
	return rule
}

// DiscoverClaudeSettings reads every ComponentClaudeSettings file and parses
// the permissions block. Files that fail JSON parse are skipped silently.
func DiscoverClaudeSettings(manifest models.ScanManifest) []models.ClaudeSettings {
	var out []models.ClaudeSettings
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentClaudeSettings {
			continue
		}
		full := filepath.Join(manifest.RepoRoot, c.Path)
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		// EndLine = number of lines in the file. Count '\n' bytes, then +1 if the
		// last line has no trailing newline (so a 5-line file with trailing \n has
		// 5 \n's → 5 lines; a 5-line file without trailing \n has 4 \n's → still 5).
		// Empty file is treated as 1 line.
		endLine := bytes.Count(raw, []byte{'\n'})
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			endLine++
		}
		if endLine == 0 {
			endLine = 1
		}
		var parsed claudeSettingsFile
		if err := json.Unmarshal(raw, &parsed); err != nil {
			continue
		}
		out = append(out, models.ClaudeSettings{
			Location: models.Location{
				FilePath: filepath.ToSlash(c.Path),
				Line:     1,
				EndLine:  endLine,
			},
			Permissions:     parsePermissionsBlock(parsed.Permissions),
			DefaultMode:     parsed.Permissions.DefaultMode,
			AdditionalDirs:  parsed.Permissions.AdditionalDirectories,
			HasEnvBlock:     rawPresent(parsed.Env),
			HasHooks:        rawPresent(parsed.Hooks),
			HasSandboxBlock: rawPresent(parsed.Sandbox),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}

type claudeSettingsFile struct {
	Permissions permissionsRaw  `json:"permissions"`
	Env         json.RawMessage `json:"env"`
	Hooks       json.RawMessage `json:"hooks"`
	Sandbox     json.RawMessage `json:"sandbox"`
}

type permissionsRaw struct {
	Allow                 []string `json:"allow"`
	Deny                  []string `json:"deny"`
	Ask                   []string `json:"ask"`
	DefaultMode           string   `json:"defaultMode"`
	AdditionalDirectories []string `json:"additionalDirectories"`
}

func parsePermissionsBlock(p permissionsRaw) models.ClaudePermissions {
	return models.ClaudePermissions{
		Allow: parseRules(p.Allow),
		Deny:  parseRules(p.Deny),
		Ask:   parseRules(p.Ask),
	}
}

func parseRules(raw []string) []models.PermissionRule {
	if len(raw) == 0 {
		return nil
	}
	out := make([]models.PermissionRule, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out = append(out, ParsePermissionRule(r))
	}
	return out
}

// computeNewlineOffsets returns the byte offsets of every '\n' in raw, in
// ascending order. Used together with lineForOffset for byte→line lookups
// in source files (e.g. attaching JSON-token positions to a 1-indexed line).
func computeNewlineOffsets(raw []byte) []int {
	var nls []int
	for i, b := range raw {
		if b == '\n' {
			nls = append(nls, i)
		}
	}
	return nls
}

// lineForOffset returns the 1-indexed line number that contains the byte at
// the given offset. nls is the slice returned by computeNewlineOffsets.
// Convention: a '\n' character itself belongs to the line it terminates,
// so a newline at offset N is on the same line as the bytes preceding it.
func lineForOffset(off int64, nls []int) int {
	// sort.SearchInts returns the smallest index i where nls[i] >= off.
	// That is the count of newlines strictly before off, except when nls[i]
	// == off (the byte IS the newline), in which case it's also the count of
	// newlines preceding it — and the byte itself sits on line i+1.
	i := sort.SearchInts(nls, int(off))
	return i + 1
}
