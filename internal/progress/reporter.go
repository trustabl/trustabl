// Package progress renders real-time scan progress to stderr without touching
// the stdout report (which must stay byte-stable per the determinism contract).
package progress

// Mode selects how progress is rendered.
type Mode int

const (
	ModeOff   Mode = iota // silent (JSON, or --no-progress)
	ModePlain             // static one-line-per-phase, no ANSI (piped human)
	ModeTTY               // animated spinner + bar (interactive human)
)

// Reporter receives progress events. All wording (labels/summaries) is supplied
// by the caller; implementations only render.
type Reporter interface {
	StartPhase(key, label string) // begin a phase
	SetTotal(n int)               // optional: switch the phase to an n-step bar
	Advance(detail string)        // tick the bar; detail = current file/entity
	EndPhase(summary string)      // finish: emit a persistent "[key] summary"
	Fatal(err error)              // a phase failed; tear the UI down cleanly
}

// PickMode resolves the render mode from output settings. isTTY reflects whether
// stderr is an interactive terminal.
func PickMode(format string, isTTY, noColor, noProgress bool) Mode {
	if format == "json" || noProgress {
		return ModeOff
	}
	if isTTY && !noColor {
		return ModeTTY
	}
	return ModePlain
}

type nopReporter struct{}

// NewNop returns a Reporter that does nothing.
func NewNop() Reporter { return nopReporter{} }

func (nopReporter) StartPhase(string, string) {}
func (nopReporter) SetTotal(int)              {}
func (nopReporter) Advance(string)            {}
func (nopReporter) EndPhase(string)           {}
func (nopReporter) Fatal(error)               {}
