// Package crash implements local-first crash reporting: it captures a scrubbed
// report on an unrecovered panic, always writes it to a local file, and offers
// an interactive, per-crash choice to send it or file an issue.
package crash

import (
	"fmt"
	"regexp"
	"strings"
)

// Meta is the build/runtime context folded into a Report.
type Meta struct {
	Version  string
	Commit   string
	OS       string
	Arch     string
	RulesSHA string // resolved rules SHA if known, else ""
}

// Report is a scrubbed crash report. It contains no argument values, no source
// lines, and no full file paths — only the information needed to debug a panic.
type Report struct {
	PanicValue string
	Stack      []string
	Version    string
	Commit     string
	OS         string
	Arch       string
	RulesSHA   string
}

// secretPatterns mask credential-shaped substrings before anything is written
// or sent. Ordered longest-prefix-first so provider keys mask fully.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`sk-proj-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`[0-9a-fA-F]{32,}`),
	regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),
}

// scrubSecrets replaces credential-shaped substrings with [REDACTED].
func scrubSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// renderStack turns raw debug.Stack() output into scrubbed frame lines: function
// arguments are dropped, file paths trimmed to basename:line, offsets removed.
// Every output line is passed through scrubSecrets before appending.
func renderStack(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.HasPrefix(ln, "goroutine ") {
			out = append(out, scrubSecrets(ln))
			continue
		}
		trimmed := strings.TrimLeft(ln, "\t")
		if trimmed != ln { // was tab-indented → a file:line frame
			field := trimmed
			if sp := strings.IndexByte(field, ' '); sp >= 0 {
				field = field[:sp] // drop " +0x1d" offset
			}
			if slash := strings.LastIndexByte(field, '/'); slash >= 0 {
				field = field[slash+1:] // trim path to basename:line
			}
			out = append(out, scrubSecrets("\t"+field))
			continue
		}
		// "created by" lines are runtime annotations — preserve them as-is
		// (after scrubbing) rather than mangling with the (...) replacement.
		if strings.HasPrefix(ln, "created by ") {
			out = append(out, scrubSecrets(ln))
			continue
		}
		// function line: keep name, replace arg list with (...)
		if op := strings.LastIndexByte(ln, '('); op >= 0 {
			out = append(out, scrubSecrets(ln[:op]+"(...)"))
		} else {
			out = append(out, scrubSecrets(ln))
		}
	}
	return out
}

// Capture builds a scrubbed Report from a recovered panic value and its stack.
func Capture(recovered any, stack []byte, meta Meta) Report {
	return Report{
		PanicValue: scrubSecrets(fmt.Sprint(recovered)),
		Stack:      renderStack(stack),
		Version:    meta.Version,
		Commit:     meta.Commit,
		OS:         meta.OS,
		Arch:       meta.Arch,
		RulesSHA:   meta.RulesSHA,
	}
}

// String renders the human-readable crash file. This is exactly what a user
// sees on disk and what "Send" transmits — the file is the transparency preview.
func (r Report) String() string {
	var b strings.Builder
	b.WriteString("Trustabl crash report\n")
	fmt.Fprintf(&b, "version: %s\ncommit: %s\nos/arch: %s/%s\nrules_sha: %s\n\n",
		r.Version, r.Commit, r.OS, r.Arch, r.RulesSHA)
	fmt.Fprintf(&b, "panic: %s\n\n", r.PanicValue)
	b.WriteString(strings.Join(r.Stack, "\n"))
	b.WriteString("\n")
	return b.String()
}

// Props is the property map for the telemetry crash.reported event.
func (r Report) Props() map[string]any {
	return map[string]any{
		"panic_value": r.PanicValue,
		"stack":       strings.Join(r.Stack, "\n"),
		"version":     r.Version,
		"commit":      r.Commit,
		"os":          r.OS,
		"arch":        r.Arch,
		"rules_sha":   r.RulesSHA,
	}
}
