package forge

import (
	"fmt"
	"strconv"
	"strings"
)

// Stamp holds provenance metadata embedded in a forge-generated SKILL.md.
type Stamp struct {
	Date   string   // YYYY-MM-DD
	SHA    string   // resolved rules commit SHA
	Schema int      // pack manifest schema_version
	SDKs   []string // sorted detected SDK IDs
}

// Line renders the stamp as an HTML comment. Returns empty string when SHA is
// empty so a zero-value Stamp silently omits the comment.
func (s Stamp) Line() string {
	if s.SHA == "" {
		return ""
	}
	return fmt.Sprintf(
		"<!-- generated: %s | rules: %s | schema: %d | sdks: %s -->",
		s.Date, s.SHA, s.Schema, strings.Join(s.SDKs, ", "),
	)
}

// ParseStamp scans content for a forge stamp HTML comment and parses its
// fields. Returns (Stamp{}, false) when no stamp is found or the comment is
// malformed.
func ParseStamp(content string) (Stamp, bool) {
	const prefix = "<!-- generated: "
	const suffix = " -->"

	idx := strings.Index(content, prefix)
	if idx < 0 {
		return Stamp{}, false
	}
	rest := content[idx+len(prefix):]
	end := strings.Index(rest, suffix)
	if end < 0 {
		return Stamp{}, false
	}
	body := rest[:end]

	// body: "2026-08-11 | rules: abc123 | schema: 13 | sdks: claude_sdk, mcp"
	parts := strings.SplitN(body, " | ", 4)
	if len(parts) != 4 {
		return Stamp{}, false
	}

	date := parts[0]

	rulesVal := strings.TrimPrefix(parts[1], "rules: ")
	if rulesVal == parts[1] { // prefix not found
		return Stamp{}, false
	}

	schemaStr := strings.TrimPrefix(parts[2], "schema: ")
	if schemaStr == parts[2] {
		return Stamp{}, false
	}
	schema, err := strconv.Atoi(schemaStr)
	if err != nil {
		return Stamp{}, false
	}

	sdksStr := strings.TrimPrefix(parts[3], "sdks: ")
	if sdksStr == parts[3] { // prefix not found
		return Stamp{}, false
	}
	var sdks []string
	for _, s := range strings.Split(sdksStr, ", ") {
		if s = strings.TrimSpace(s); s != "" {
			sdks = append(sdks, s)
		}
	}

	return Stamp{Date: date, SHA: rulesVal, Schema: schema, SDKs: sdks}, true
}
