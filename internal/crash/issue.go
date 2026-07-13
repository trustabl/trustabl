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

// IssueURL builds a pre-filled GitHub "new issue" URL. It deliberately omits the
// stack trace (GitHub truncates long query strings ~8KB) — the body points the
// user at their local crash file instead.
func IssueURL(r Report) string {
	title := "Crash: " + shortPanic(r.PanicValue)
	body := fmt.Sprintf(
		"**Version:** %s\n**Commit:** %s\n**OS/Arch:** %s/%s\n**Rules SHA:** %s\n\n"+
			"Please paste the contents of your local crash report file below "+
			"(printed above when Trustabl crashed):\n\n```\n\n```\n",
		r.Version, r.Commit, r.OS, r.Arch, r.RulesSHA)
	q := url.Values{}
	q.Set("title", title)
	q.Set("body", body)
	q.Set("labels", "crash")
	return "https://github.com/trustabl/trustabl/issues/new?" + q.Encode()
}
