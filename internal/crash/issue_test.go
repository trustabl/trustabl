package crash

import (
	"net/url"
	"strings"
	"testing"
)

func TestIssueURL(t *testing.T) {
	r := Report{
		PanicValue: "runtime error: index out of range [5] with length 3\nextra line",
		Stack:      []string{"main.f(...)", "\tscan.go:201"},
		Version:    "1.2.3", Commit: "abc", OS: "darwin", Arch: "arm64",
	}
	u := IssueURL(r, "/home/u/.config/trustabl/crash-x.log")
	if !strings.HasPrefix(u, "https://github.com/trustabl/trustabl/issues/new?") {
		t.Fatalf("unexpected base: %s", u)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	if !strings.HasPrefix(q.Get("title"), "Crash: runtime error") {
		t.Fatalf("title = %q", q.Get("title"))
	}
	if strings.Contains(q.Get("title"), "\n") {
		t.Fatalf("title must be single-line: %q", q.Get("title"))
	}
	body := q.Get("body")
	if !strings.Contains(body, "1.2.3") || !strings.Contains(body, "darwin/arm64") {
		t.Fatalf("body missing meta: %q", body)
	}
	// The scrubbed stack IS now embedded so the user need not paste anything.
	if !strings.Contains(body, "main.f(...)") || !strings.Contains(body, "scan.go:201") {
		t.Fatalf("body should embed the scrubbed stack: %q", body)
	}
	if !strings.Contains(body, "panic: runtime error") {
		t.Fatalf("body should embed the panic value: %q", body)
	}
	// A small report fits — no truncation notice.
	if strings.Contains(body, "truncated") {
		t.Fatalf("small report should not be truncated: %q", body)
	}
}

func TestIssueURLTruncatesLargeStack(t *testing.T) {
	stack := make([]string, 2000)
	for i := range stack {
		stack[i] = "\tinternal/analysis/discovery.go:1234"
	}
	r := Report{
		PanicValue: "boom",
		Stack:      stack,
		Version:    "1.2.3", Commit: "abc", OS: "darwin", Arch: "arm64",
	}
	path := "/home/u/.config/trustabl/crash-x.log"
	u := IssueURL(r, path)
	if len(u) > maxIssueURLLen {
		t.Fatalf("URL length %d exceeds cap %d", len(u), maxIssueURLLen)
	}
	body, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := body.Query().Get("body")
	if !strings.Contains(b, "truncated") || !strings.Contains(b, path) {
		t.Fatalf("truncated body should point at local file: %q", b)
	}
}
