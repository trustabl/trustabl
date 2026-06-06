// Package logx is a tiny leveled diagnostic logger for the CLI.
//
// It writes ONLY to the configured writer (always os.Stderr in production) and
// never to stdout, so it cannot perturb the byte-stable human report or the
// JSON/SARIF streams — the determinism contract (see the repo CLAUDE.md). The
// progress package owns the phase UI on stderr; logx owns opt-in verbose/debug
// detail. They are separate streams of intent that happen to share stderr.
//
// A nil *Logger is a valid, silent logger: every method short-circuits before
// touching any field, so call sites can hold a nil logger (the zero value of a
// Config.Log field) without guarding each call.
package logx

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Level is the diagnostic verbosity. Each level includes everything below it,
// so LevelDebug also emits verbose lines.
type Level int

const (
	LevelNormal  Level = iota // default: no diagnostics
	LevelVerbose              // --verbose / -v: provenance, discovery counts, phase summaries
	LevelDebug                // --debug: everything verbose shows, plus timing and per-entity / per-finding detail
)

// ANSI dim/reset, applied to the level tag only when color is enabled so the
// diagnostic prefix recedes behind the actual report. The message body is left
// unstyled. The tag text is ASCII so stripping color leaves readable output.
const (
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

// Logger writes leveled diagnostic lines to an io.Writer (stderr). It is safe
// for concurrent use: in the non-verbose TTY path the scan runs on a goroutine
// while the main goroutine renders, and the mutex keeps their writes whole.
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
	color bool
}

// New returns a Logger writing to w at the given level. color enables a dim
// ANSI tag on each line; pass false (for --no-color, a non-terminal, or a
// server log) for plain ASCII output. A LevelNormal logger satisfies every
// method but emits nothing.
func New(w io.Writer, level Level, color bool) *Logger {
	return &Logger{w: w, level: level, color: color}
}

// Enabled reports whether a message at lvl would be emitted. Use it to guard
// expensive message construction, e.g. a capped per-entity debug dump. Returns
// false for a nil logger and for LevelNormal (which is never emitted).
func (l *Logger) Enabled(lvl Level) bool {
	return l != nil && lvl > LevelNormal && l.level >= lvl
}

// Verbose reports whether verbose (or debug) output is on. Callers use it to
// decide unrelated behavior — notably downgrading the animated progress panel
// to plain lines so it does not fight interleaved log lines on stderr.
func (l *Logger) Verbose() bool { return l.Enabled(LevelVerbose) }

// Verbosef emits one line at verbose level (also shown under --debug).
func (l *Logger) Verbosef(format string, args ...any) {
	l.emit(LevelVerbose, "verbose", format, args...)
}

// Debugf emits one line at debug level only.
func (l *Logger) Debugf(format string, args ...any) {
	l.emit(LevelDebug, "debug", format, args...)
}

// Timer starts a debug-level stopwatch for a named operation and returns a stop
// function that logs the elapsed time. When debug logging is off it reads no
// clock and the returned stop is a no-op, so timing is free on the normal path
// and cannot introduce a time-dependent value anywhere near the report. Typical
// use: defer log.Timer("phase")().
func (l *Logger) Timer(name string) func() {
	if !l.Enabled(LevelDebug) {
		return func() {}
	}
	start := time.Now()
	// Microsecond resolution: millisecond rounding renders a sub-ms phase as a
	// confusing "0s", while nanoseconds are noise. µs reads cleanly across the
	// range a scan spans (a fast phase as "412µs", a slow one as "1.4s").
	return func() { l.Debugf("%s took %s", name, time.Since(start).Round(time.Microsecond)) }
}

func (l *Logger) emit(lvl Level, tag, format string, args ...any) {
	if !l.Enabled(lvl) {
		return
	}
	prefix := "[" + tag + "] "
	if l.color {
		prefix = ansiDim + "[" + tag + "]" + ansiReset + " "
	}
	line := prefix + fmt.Sprintf(format, args...) + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(l.w, line)
}
