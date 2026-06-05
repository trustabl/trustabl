package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		// Per-rule line numbers via positional walk. Failure is non-fatal:
		// if the walker errors (which would mean the file is structurally
		// malformed in a way json.Unmarshal didn't catch — very unlikely),
		// we still emit the rules without line numbers. The unmarshal pass
		// above is the authoritative correctness check.
		allowLines, denyLines, askLines, lineErr := extractPermissionLines(raw)

		perms := parsePermissionsBlock(parsed.Permissions)
		if lineErr == nil {
			zipLines(perms.Allow, allowLines)
			zipLines(perms.Deny, denyLines)
			zipLines(perms.Ask, askLines)
		}
		out = append(out, models.ClaudeSettings{
			Location: models.Location{
				FilePath: filepath.ToSlash(c.Path),
				Line:     1,
				EndLine:  endLine,
			},
			Permissions:     perms,
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

// expectDelim reads the next token and asserts it is the given delimiter.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return fmt.Errorf("expected delim %q, got %v", want, tok)
	}
	return nil
}

// readKey reads the next token and asserts it is a string (object key).
func readKey(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	s, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("expected key string, got %T", tok)
	}
	return s, nil
}

// maxJSONDepth bounds the skipValue/skipToCloser recursion. A real settings.json
// nests a few levels; a pathologically deep object/array (a hostile file) would
// otherwise exhaust the goroutine stack during this line-attribution walk. The
// json.Unmarshal pass elsewhere is the authoritative structural check; this walk
// only needs to fail gracefully (the caller then emits rules without line
// numbers), so exceeding the bound returns an error rather than crashing.
const maxJSONDepth = 1000

// skipToCloser consumes tokens until the matching closing delim of an
// already-consumed opening delim `opened` ('{' or '['). Handles nested
// objects/arrays. Object keys must be consumed as part of the walk.
func skipToCloser(dec *json.Decoder, opened json.Delim, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("json nesting exceeds maximum depth %d", maxJSONDepth)
	}
	closer := json.Delim('}')
	if opened == '[' {
		closer = ']'
	}
	for dec.More() {
		if opened == '{' {
			if _, err := dec.Token(); err != nil { // key
				return err
			}
		}
		if err := skipValue(dec, depth); err != nil {
			return err
		}
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != closer {
		return fmt.Errorf("expected closer %q, got %v", closer, tok)
	}
	return nil
}

// skipValue consumes one complete JSON value from the decoder, regardless
// of shape. Used to advance past values whose content is irrelevant.
func skipValue(dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		return skipToCloser(dec, d, depth+1)
	}
	return nil
}

// readStringArrayLines walks an array, recording the line of each string
// item's opening quote. The decoder must be positioned at the opening '['.
// Non-string items (objects, arrays, numbers) are skipped without recording.
func readStringArrayLines(dec *json.Decoder, nls []int) ([]int, error) {
	if err := expectDelim(dec, '['); err != nil {
		return nil, err
	}
	var lines []int
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if s, ok := tok.(string); ok {
			// InputOffset() after reading a string token points to the byte
			// after the closing '"'. The JSON-encoded form is len(`"` + escaped + `"`)
			// bytes. Subtract that length to get the offset of the opening '"'.
			//
			// Caveat: json.Marshal HTML-escapes '<', '>', '&' by default
			// (e.g. '<' → <, 6 bytes), so if the source file contains
			// any of those literally in a permission rule the recovered
			// startOff is off and the line attribution may shift to a
			// nearby rule. Claude permission rules in practice are ASCII
			// identifiers + ()*/_-:., none of which trigger HTML-escape,
			// so this is a latent edge case rather than a defect on real
			// settings.json inputs.
			endOff := dec.InputOffset()
			encoded, _ := json.Marshal(s)
			startOff := endOff - int64(len(encoded))
			lines = append(lines, lineForOffset(startOff, nls))
			continue
		}
		// Non-string item: if it was a delim, drain until the matching close.
		if d, ok := tok.(json.Delim); ok {
			if err := skipToCloser(dec, d, 0); err != nil {
				return nil, err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // consume closing ']'
		return nil, err
	}
	return lines, nil
}

// zipLines copies up to len(rules) line numbers from lines into each rule's
// Line field. If lines is shorter than rules (shouldn't happen for valid
// JSON that round-tripped through json.Unmarshal AND extractPermissionLines),
// the trailing rules keep Line=0.
func zipLines(rules []models.PermissionRule, lines []int) {
	for i := range rules {
		if i >= len(lines) {
			break
		}
		rules[i].Line = lines[i]
	}
}

// extractPermissionLines does a streaming token walk over raw JSON to find
// the line of each string literal inside permissions.{allow,deny,ask}. It
// returns three slices in the same order as the array entries. Any JSON
// parse failure is returned so the caller can fall back to emitting rules
// without line numbers; the standard json.Unmarshal pass elsewhere is the
// authoritative correctness check for the file's structure.
func extractPermissionLines(raw []byte) (allow, deny, ask []int, err error) {
	nls := computeNewlineOffsets(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err = expectDelim(dec, '{'); err != nil {
		return nil, nil, nil, err
	}
	for dec.More() {
		key, kerr := readKey(dec)
		if kerr != nil {
			return nil, nil, nil, kerr
		}
		if key != "permissions" {
			if serr := skipValue(dec, 0); serr != nil {
				return nil, nil, nil, serr
			}
			continue
		}
		if err = expectDelim(dec, '{'); err != nil {
			return nil, nil, nil, err
		}
		for dec.More() {
			subKey, kerr := readKey(dec)
			if kerr != nil {
				return nil, nil, nil, kerr
			}
			var lines []int
			var perr error
			switch subKey {
			case "allow":
				lines, perr = readStringArrayLines(dec, nls)
				allow = lines
			case "deny":
				lines, perr = readStringArrayLines(dec, nls)
				deny = lines
			case "ask":
				lines, perr = readStringArrayLines(dec, nls)
				ask = lines
			default:
				perr = skipValue(dec, 0)
			}
			if perr != nil {
				return nil, nil, nil, perr
			}
		}
		// consume closing '}' of permissions; we don't need to walk further.
		if _, err = dec.Token(); err != nil {
			return nil, nil, nil, err
		}
		return allow, deny, ask, nil
	}
	return nil, nil, nil, nil
}
