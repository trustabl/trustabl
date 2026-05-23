package progress

import "testing"

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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pickMode(c.format, c.isTTY, c.noColor, c.noProgress); got != c.want {
				t.Errorf("pickMode(%q,%v,%v,%v) = %v, want %v",
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
