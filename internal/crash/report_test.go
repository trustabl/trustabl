package crash

import (
	"strings"
	"testing"
)

func TestScrubSecrets(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"anthropic key": {"key sk-ant-api03-abc123DEF456 leaked", "key [REDACTED] leaked"},
		"openai proj":   {"sk-proj-ZZZ999aaa here", "[REDACTED] here"},
		"long hex":      {"digest deadbeefdeadbeefdeadbeefdeadbeef00", "digest [REDACTED]"},
		"plain text":    {"nil pointer dereference", "nil pointer dereference"},
	}
	for name, c := range cases {
		if got := scrubSecrets(c.in); got != c.want {
			t.Errorf("%s: scrubSecrets(%q) = %q, want %q", name, c.in, got, c.want)
		}
	}
}

func TestRenderStackDropsArgsAndPaths(t *testing.T) {
	raw := []byte("goroutine 1 [running]:\n" +
		"main.(*runner).do(0xc0000a2, 0x1)\n" +
		"\t/Users/dev/secret-project/cmd/trustabl/scan.go:241 +0x1d\n")
	got := renderStack(raw)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "0xc0000a2") {
		t.Errorf("stack kept argument values: %q", joined)
	}
	if strings.Contains(joined, "secret-project") {
		t.Errorf("stack kept full path: %q", joined)
	}
	if !strings.Contains(joined, "main.(*runner).do(...)") {
		t.Errorf("stack lost function name: %q", joined)
	}
	if !strings.Contains(joined, "scan.go:241") {
		t.Errorf("stack lost basename:line: %q", joined)
	}
}

func TestCaptureAndProps(t *testing.T) {
	r := Capture("boom", []byte("goroutine 1 [running]:\nmain.f()\n\t/x/y/main.go:1 +0x1\n"),
		Meta{Version: "1.2.3", Commit: "abc", OS: "darwin", Arch: "arm64", RulesSHA: "sha1"})
	if r.PanicValue != "boom" {
		t.Fatalf("PanicValue = %q", r.PanicValue)
	}
	p := r.Props()
	if p["version"] != "1.2.3" || p["os"] != "darwin" || p["rules_sha"] != "sha1" {
		t.Fatalf("Props missing meta: %#v", p)
	}
	if _, ok := p["stack"].(string); !ok {
		t.Fatalf("Props[stack] not a string: %#v", p["stack"])
	}
}

func TestCaptureScrubsSecrets(t *testing.T) {
	// Use obviously-fake placeholder secrets — not real credential values.
	// sk-proj-FAKEfake456 is placed in the goroutine header so it survives
	// arg-stripping and must be masked by scrubSecrets.
	panicMsg := "boom sk-ant-api03-FAKEfake123"
	stackWithSecret := []byte(
		"goroutine 1 [running]: sk-proj-FAKEfake456\n" +
			"main.f(0x1)\n" +
			"\t/x/y/main.go:1 +0x1\n",
	)

	r := Capture(panicMsg, stackWithSecret, Meta{})

	if strings.Contains(r.PanicValue, "sk-ant-api03-FAKEfake123") {
		t.Errorf("PanicValue still contains raw secret: %q", r.PanicValue)
	}
	if !strings.Contains(r.PanicValue, "[REDACTED]") {
		t.Errorf("PanicValue missing [REDACTED]: %q", r.PanicValue)
	}

	joined := strings.Join(r.Stack, "\n")
	if strings.Contains(joined, "sk-proj-FAKEfake456") {
		t.Errorf("Stack still contains raw secret: %q", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Errorf("Stack missing [REDACTED]: %q", joined)
	}
}

func TestStringHasHeader(t *testing.T) {
	r := Report{
		PanicValue: "nil pointer dereference",
		Stack:      []string{"goroutine 1 [running]:", "main.f(...)"},
		Version:    "0.0.1",
		OS:         "linux",
		Arch:       "amd64",
	}
	s := r.String()
	if !strings.Contains(s, "Trustabl crash report") {
		t.Errorf("String() missing header line; got:\n%s", s)
	}
}
