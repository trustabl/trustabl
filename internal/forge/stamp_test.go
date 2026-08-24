package forge

import (
	"testing"
)

func TestStamp_Line_WithSHA(t *testing.T) {
	s := Stamp{Date: "2026-08-11", SHA: "abc1234", Schema: 13, SDKs: []string{"claude_sdk", "mcp"}}
	want := "<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: claude_sdk, mcp -->"
	if got := s.Line(); got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func TestStamp_Line_EmptySHA(t *testing.T) {
	s := Stamp{Date: "2026-08-11", Schema: 13, SDKs: []string{"claude_sdk"}}
	if got := s.Line(); got != "" {
		t.Errorf("Line() with empty SHA = %q, want empty string", got)
	}
}

func TestStamp_Line_NoSDKs(t *testing.T) {
	s := Stamp{Date: "2026-08-11", SHA: "abc1234", Schema: 13}
	want := "<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks:  -->"
	if got := s.Line(); got != want {
		t.Errorf("Line() with nil SDKs = %q, want %q", got, want)
	}
}

func TestParseStamp_RoundTrip(t *testing.T) {
	orig := Stamp{
		Date:   "2026-08-11",
		SHA:    "abc1234def5678",
		Schema: 13,
		SDKs:   []string{"claude_sdk", "mcp"},
	}
	content := "# Header\n\n" + orig.Line() + "\n\nsome body text"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false for valid stamp")
	}
	if got.Date != orig.Date {
		t.Errorf("Date: got %q, want %q", got.Date, orig.Date)
	}
	if got.SHA != orig.SHA {
		t.Errorf("SHA: got %q, want %q", got.SHA, orig.SHA)
	}
	if got.Schema != orig.Schema {
		t.Errorf("Schema: got %d, want %d", got.Schema, orig.Schema)
	}
	if len(got.SDKs) != len(orig.SDKs) {
		t.Errorf("SDKs: got %v, want %v", got.SDKs, orig.SDKs)
	}
}

func TestParseStamp_NoStamp(t *testing.T) {
	cases := []string{
		"",
		"# Just a regular markdown file\n\nNo stamp here.",
		"<!-- some other comment -->",
		"<!-- generated: bad | format -->",
	}
	for _, c := range cases {
		if _, ok := ParseStamp(c); ok {
			t.Errorf("ParseStamp(%q) returned true, want false", c)
		}
	}
}

func TestParseStamp_MidFile(t *testing.T) {
	// Stamp can appear anywhere in the content, not just the first line.
	s := Stamp{Date: "2026-01-01", SHA: "deadbeef", Schema: 1, SDKs: []string{"openai_sdk"}}
	content := "lots of content before\n\n" + s.Line() + "\n\nlots of content after"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false")
	}
	if got.SHA != s.SHA {
		t.Errorf("SHA: got %q, want %q", got.SHA, s.SHA)
	}
}

func TestParseStamp_MalformedSDKsPrefix(t *testing.T) {
	// A 4th segment without the "sdks: " prefix is a malformed stamp and must
	// be rejected — mirroring the rules:/schema: prefix guards — rather than
	// silently absorbing the raw segment into the SDK list.
	content := "<!-- generated: 2026-08-11 | rules: abc123 | schema: 13 | claude_sdk, mcp -->"
	if got, ok := ParseStamp(content); ok {
		t.Errorf("ParseStamp returned true for missing sdks: prefix, got %+v", got)
	}
}
