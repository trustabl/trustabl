package analysis

import (
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// splitToolGrants splits a frontmatter tool list into individual grant tokens.
// It splits on commas AND ASCII whitespace, but only at parenthesis depth zero,
// so a parametered grant like "Agent(worker, researcher)" or "Bash(npm run *)"
// survives as one token. This handles every serialization seen in the wild:
// comma-separated subagent `tools:`, space-separated skill `allowed-tools:`,
// and bare YAML-list entries. Empty tokens are dropped; returns nil when empty.
func splitToolGrants(raw string) []string {
	var out []string
	var buf strings.Builder
	depth := 0
	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}
	for _, r := range raw {
		switch r {
		case '(':
			depth++
			buf.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			buf.WriteRune(r)
		case ',', ' ', '\t', '\n', '\r':
			if depth == 0 {
				flush()
			} else {
				buf.WriteRune(r)
			}
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

// parseToolGrants parses raw grant tokens into typed ToolGrants using the
// settings.json permission grammar (ParsePermissionRule). An unparseable token
// keeps its raw text as the Tool name so nothing is silently dropped.
func parseToolGrants(tokens []string) []models.ToolGrant {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]models.ToolGrant, 0, len(tokens))
	for _, tok := range tokens {
		pr := ParsePermissionRule(tok)
		tool := pr.Tool
		if tool == "" {
			tool = tok
		}
		out = append(out, models.ToolGrant{Tool: tool, Pattern: pr.Pattern, Raw: tok})
	}
	return out
}
