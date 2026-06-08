package progress

import (
	"fmt"
	"io"
)

// plainReporter writes one static "[key] summary" line per finished phase. No
// ANSI, no animation — safe for piped output and CI logs.
type plainReporter struct {
	w   io.Writer
	key string
}

// NewPlain returns a Reporter that writes static phase-summary lines to w.
func NewPlain(w io.Writer) Reporter { return &plainReporter{w: w} }

// StartPhase prints a line immediately so a long-running phase (notably the
// network pre-flight: rules resolution, remote clone) is not a blank screen
// until it finishes — the verified cause of the "nothing displayed" pre-flight.
func (r *plainReporter) StartPhase(key, label string) {
	r.key = key
	// ASCII "..." not "…": plain mode is the piped/CI path, where output may land
	// in a non-UTF-8 console or log where U+2026 mojibakes.
	fmt.Fprintf(r.w, "[%s] %s...\n", key, label)
}
func (r *plainReporter) SetTotal(int)                {}
func (r *plainReporter) Advance(string)              {}
func (r *plainReporter) SetDetail(string)            {}
func (r *plainReporter) SetProgress(float64, string) {}
func (r *plainReporter) ResetPhase()                 {}
func (r *plainReporter) EndPhase(summary string)     { fmt.Fprintf(r.w, "[%s] %s\n", r.key, summary) }
func (r *plainReporter) Fatal(err error)             { fmt.Fprintf(r.w, "[%s] failed: %v\n", r.key, err) }
