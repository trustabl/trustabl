package crash

import (
	"fmt"
	"net/url"
	"strings"
)

// shortPanic returns a single-line, length-capped summary of the panic value
// for use as an issue title.
func shortPanic(panicValue string) string {
	s := panicValue
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[:nl]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return strings.TrimSpace(s)
}

// maxIssueURLLen caps the generated new-issue URL. GitHub rejects overly long
// query strings (~8KB → HTTP 414 / silent truncation), so we stay comfortably
// under that and truncate the embedded stack if a crash is unusually large.
const maxIssueURLLen = 7000

// IssueURL builds a pre-filled GitHub "new issue" URL with the scrubbed crash
// report embedded in the body — no manual paste required. The report is already
// safe to share (frames only, no args, no source, secrets redacted). If the
// full report would push the URL past maxIssueURLLen, the stack is truncated and
// the body points the user at their complete local crash file (path).
func IssueURL(r Report, path string) string {
	title := "Crash: " + shortPanic(r.PanicValue)

	// Drop stack frames from the end until the URL fits GitHub's limit. An
	// empty stack that still overflows (a huge panic value) degrades to the
	// best-effort URL rather than looping forever.
	stack := r.Stack
	for {
		u := buildIssueURL(title, r, stack, len(stack) < len(r.Stack), path)
		if len(u) <= maxIssueURLLen || len(stack) == 0 {
			return u
		}
		drop := len(stack) / 10
		if drop < 1 {
			drop = 1
		}
		stack = stack[:len(stack)-drop]
	}
}

// buildIssueURL assembles the URL for a (possibly truncated) stack slice.
func buildIssueURL(title string, r Report, stack []string, truncated bool, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"**Version:** %s\n**Commit:** %s\n**OS/Arch:** %s/%s\n**Rules SHA:** %s\n\n"+
			"Crash report captured automatically — no source code or file contents, "+
			"secrets redacted:\n\n```\npanic: %s\n\n%s\n",
		r.Version, r.Commit, r.OS, r.Arch, r.RulesSHA, r.PanicValue, strings.Join(stack, "\n"))
	if truncated {
		fmt.Fprintf(&b, "\n[stack truncated to fit GitHub's URL limit — full report at %s]\n", path)
	}
	b.WriteString("```\n")

	q := url.Values{}
	q.Set("title", title)
	q.Set("body", b.String())
	q.Set("labels", "crash")
	return "https://github.com/trustabl/trustabl/issues/new?" + q.Encode()
}
