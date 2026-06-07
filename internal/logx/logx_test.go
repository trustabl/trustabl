package logx

import (
	"bytes"
	"strings"
	"testing"
)

// At each level, only messages at or below that level are emitted, and
// LevelNormal stays completely silent.
func TestLevelGating(t *testing.T) {
	cases := []struct {
		level       Level
		wantVerbose bool
		wantDebug   bool
	}{
		{LevelNormal, false, false},
		{LevelVerbose, true, false},
		{LevelDebug, true, true},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		l := New(&buf, c.level, false)
		l.Verbosef("v-line")
		l.Debugf("d-line")
		out := buf.String()
		if got := strings.Contains(out, "v-line"); got != c.wantVerbose {
			t.Errorf("level %d: verbose emitted=%v, want %v (out=%q)", c.level, got, c.wantVerbose, out)
		}
		if got := strings.Contains(out, "d-line"); got != c.wantDebug {
			t.Errorf("level %d: debug emitted=%v, want %v (out=%q)", c.level, got, c.wantDebug, out)
		}
	}
}

// A nil *Logger is a silent no-op: every method must be safe to call.
func TestNilLoggerIsSafeAndSilent(t *testing.T) {
	var l *Logger // nil
	// None of these may panic.
	l.Verbosef("x")
	l.Debugf("y")
	stop := l.Timer("z")
	stop()
	if l.Enabled(LevelVerbose) || l.Enabled(LevelDebug) || l.Verbose() {
		t.Error("nil logger reported enabled")
	}
}

// LevelNormal is never enabled, even when asked about itself.
func TestNormalNeverEnabled(t *testing.T) {
	l := New(&bytes.Buffer{}, LevelDebug, false)
	if l.Enabled(LevelNormal) {
		t.Error("Enabled(LevelNormal) = true, want false (normal is never emitted)")
	}
}

// Each emitted line carries a greppable level tag and a trailing newline.
func TestLineFormat(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelDebug, false)
	l.Verbosef("hello %d", 42)
	got := buf.String()
	if got != "[verbose] hello 42\n" {
		t.Errorf("line = %q, want %q", got, "[verbose] hello 42\n")
	}
}

// color=false emits no ANSI escapes; color=true wraps the tag in a dim escape.
func TestColorToggle(t *testing.T) {
	var plain, colored bytes.Buffer
	New(&plain, LevelVerbose, false).Verbosef("m")
	New(&colored, LevelVerbose, true).Verbosef("m")

	if strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("no-color output contains ANSI: %q", plain.String())
	}
	if !strings.Contains(colored.String(), ansiDim) {
		t.Errorf("colored output missing dim escape: %q", colored.String())
	}
	// The message body is identical once color is stripped.
	if strings.Contains(colored.String(), "m\n") != true {
		t.Errorf("colored output missing message body: %q", colored.String())
	}
}

// Timer is a no-op (emits nothing) below debug, and emits one line at debug.
func TestTimer(t *testing.T) {
	var off bytes.Buffer
	New(&off, LevelVerbose, false).Timer("phase")() // verbose < debug: silent
	if off.Len() != 0 {
		t.Errorf("Timer emitted at verbose level: %q", off.String())
	}

	var on bytes.Buffer
	New(&on, LevelDebug, false).Timer("phase")()
	if !strings.Contains(on.String(), "phase took") {
		t.Errorf("Timer at debug = %q, want a 'phase took ...' line", on.String())
	}
}
