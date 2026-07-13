package crash

import (
	"net/url"
	"strings"
	"testing"
)

func TestIssueURL(t *testing.T) {
	r := Report{
		PanicValue: "runtime error: index out of range [5] with length 3\nextra line",
		Stack:      []string{"main.f(...)"},
		Version:    "1.2.3", Commit: "abc", OS: "darwin", Arch: "arm64",
	}
	u := IssueURL(r)
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
	// The full stack must NOT be embedded — the local file carries it.
	if strings.Contains(body, "main.f(...)") {
		t.Fatalf("body should not embed the stack: %q", body)
	}
}
