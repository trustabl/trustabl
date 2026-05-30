package progress

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPickMode(t *testing.T) {
	cases := []struct {
		name                       string
		format                     string
		isTTY, noColor, noProgress bool
		want                       Mode
	}{
		{"json is always off", "json", true, false, false, ModeOff},
		{"no-progress forces off", "human", true, false, true, ModeOff},
		{"human tty colored is tty", "human", true, false, false, ModeTTY},
		{"human piped is plain", "human", false, false, false, ModePlain},
		{"human no-color is plain even on tty", "human", true, true, false, ModePlain},
		{"json beats everything", "json", false, true, true, ModeOff},
		{"sarif is always off", "sarif", true, false, false, ModeOff},
		{"sarif beats no-progress flag", "sarif", false, true, false, ModeOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PickMode(c.format, c.isTTY, c.noColor, c.noProgress); got != c.want {
				t.Errorf("PickMode(%q,%v,%v,%v) = %v, want %v",
					c.format, c.isTTY, c.noColor, c.noProgress, got, c.want)
			}
		})
	}
}

func TestNopWritesNothing(t *testing.T) {
	r := NewNop()
	r.StartPhase("recon", "Recon")
	r.SetTotal(3)
	r.Advance("x")
	r.EndPhase("done")
}

// Plain mode prints a start line per phase (so a long network pre-flight is not
// a blank screen) and a summary line at the end. Per-item Advance must NOT print
// (it would flood CI logs).
func TestPlainWritesStartAndSummaryLines(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	r.StartPhase("rules", "Resolving rules")
	r.SetDetail("fetching x") // must NOT print
	r.EndPhase("a3a1502 (cached, offline)")
	r.StartPhase("inventory", "Inventory")
	r.SetTotal(18)
	r.Advance("agent_loop.py") // must NOT print
	r.EndPhase("7 tools · 2 agents")

	got := buf.String()
	want := "[rules] Resolving rules...\n[rules] a3a1502 (cached, offline)\n" +
		"[inventory] Inventory...\n[inventory] 7 tools · 2 agents\n"
	if got != want {
		t.Errorf("plain output =\n%q\nwant\n%q", got, want)
	}
}

func TestPlainFatalWritesError(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	r.StartPhase("rules", "Resolving rules")
	r.Fatal(errors.New("boom"))
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("Fatal output %q missing error", buf.String())
	}
}
