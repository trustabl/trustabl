package progress

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

// ErrInterrupted is returned by (*TTYReporter).Run when the user interrupts the
// render loop (Ctrl-C). The caller translates it into a clean exit instead of
// surfacing bubbletea's raw kill error.
var ErrInterrupted = errors.New("progress: interrupted")

// Stage markers: a green check for a completed stage, a red cross for a failed
// one.
var (
	checkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	crossStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
)

// completedLine renders a finished stage as "✔ <label>  <summary>". The summary
// is omitted when empty, or when the label already ends with it (the clone label
// is "Cloning <url>" and its summary is the same <url>, which must not be
// doubled). HasSuffix rather than Contains so an unrelated summary that merely
// appears mid-label isn't silently dropped.
func completedLine(label, summary string) string {
	line := checkStyle.Render("✔") + " " + label
	if summary != "" && !strings.HasSuffix(label, summary) {
		line += "  " + summary
	}
	return line
}

// Messages mirror the Reporter methods; the TTYReporter sends them via p.Send.
type startPhaseMsg struct{ key, label string }
type setTotalMsg struct{ n int }
type advanceMsg struct{ detail string }
type setDetailMsg struct{ detail string }
type setProgressMsg struct {
	fraction float64
	detail   string
}
type resetPhaseMsg struct{}
type endPhaseMsg struct{ summary string }
type doneMsg struct{}
type fatalMsg struct{ err error }

// stage lifecycle states.
const (
	stageRunning = iota
	stageDone
	stageFailed
)

// stage is one pipeline step, shown as a row in the live panel.
type stage struct {
	key, label   string
	state        int
	total, count int
	fraction     float64 // determinate 0..1 fill when hasFraction (byte-driven download bar)
	hasFraction  bool
	detail       string
	summary      string
	err          error
}

// model is the bubbletea model. It keeps every stage and repaints them all as
// one in-place panel: finished rows show a ✔, the active row its
// spinner/bar, a failed row a ✗. On quit the final panel is printed once via
// tea.Println so it persists in scrollback.
type model struct {
	spinner  spinner.Model
	bar      progress.Model
	stages   []*stage
	cur      int // index of the running stage, or -1 when none is active
	width    int // terminal width (0 until the first WindowSizeMsg); rows fit to it
	quitting bool
}

func newModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	b := progress.New(progress.WithGradient("#0d9488", "#5eead4"), progress.WithWidth(24))
	// Thinner bar: light/heavy horizontal lines instead of the default
	// full/▒ blocks, so the bar reads as a slim rule rather than a tall band.
	b.Full = '━'
	b.Empty = '─'
	return model{spinner: s, bar: b, cur: -1}
}

// current returns the active stage, or nil if none is running.
func (m model) current() *stage {
	if m.cur < 0 || m.cur >= len(m.stages) {
		return nil
	}
	return m.stages[m.cur]
}

func (m model) Init() tea.Cmd { return m.spinner.Tick }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case startPhaseMsg:
		m.stages = append(m.stages, &stage{key: msg.key, label: msg.label, state: stageRunning})
		m.cur = len(m.stages) - 1
		return m, nil
	case setTotalMsg:
		if s := m.current(); s != nil {
			s.total, s.hasFraction = msg.n, false
		}
		return m, nil
	case advanceMsg:
		if s := m.current(); s != nil {
			s.count++
			s.hasFraction = false
			s.detail = msg.detail
		}
		return m, nil
	case setDetailMsg:
		if s := m.current(); s != nil {
			s.detail = msg.detail
		}
		return m, nil
	case setProgressMsg:
		// Determinate fraction bar (byte-driven download): set an explicit 0..1
		// fill so the bar climbs smoothly with the bytes, not in per-step jumps.
		if s := m.current(); s != nil {
			f := msg.fraction
			if f < 0 {
				f = 0
			} else if f > 1 {
				f = 1
			}
			s.fraction, s.hasFraction, s.detail = f, true, msg.detail
		}
		return m, nil
	case resetPhaseMsg:
		// Drop the active phase back to a bare spinner: a counted fetch that
		// failed and is now retrying via an uncounted path must not leave a stale
		// bar frozen mid-fill (the bar branch in renderStage outranks detail, so
		// total/count have to be cleared, not just the detail overwritten).
		if s := m.current(); s != nil {
			s.total, s.count, s.detail, s.hasFraction = 0, 0, "", false
		}
		return m, nil
	case endPhaseMsg:
		if s := m.current(); s != nil {
			s.state = stageDone
			s.summary = msg.summary
		}
		m.cur = -1
		return m, nil
	case fatalMsg:
		if s := m.current(); s != nil {
			s.state = stageFailed
			s.err = msg.err
		}
		m.cur = -1
		m.quitting = true
		return m, tea.Sequence(tea.Println(m.staticView()), tea.Quit)
	case doneMsg:
		m.quitting = true
		return m, tea.Sequence(tea.Println(m.staticView()), tea.Quit)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	// On quit the persistent panel was already printed via tea.Println; clear
	// the live region so it isn't duplicated.
	if m.quitting {
		return ""
	}
	rows := make([]string, len(m.stages))
	for i, s := range m.stages {
		rows[i] = m.fit(m.renderStage(s, true))
	}
	return strings.Join(rows, "\n")
}

// staticView renders the final panel with no spinner, for the one-time
// tea.Println on quit (so the completed panel persists in scrollback).
func (m model) staticView() string {
	rows := make([]string, len(m.stages))
	for i, s := range m.stages {
		rows[i] = m.fit(m.renderStage(s, false))
	}
	return strings.Join(rows, "\n")
}

// fit truncates a rendered row to the terminal width so a long detail (deep
// file path) or error never wraps — a wrapped row throws off bubbletea's
// in-place repaint and leaves orphaned lines on every subsequent tick. The
// truncate is ANSI-aware (it preserves the styled check/cross, spinner, and bar
// gradient while counting only visible width). width 0 (before the first
// WindowSizeMsg, e.g. in unit tests) means "unknown" → no truncation.
func (m model) fit(line string) string {
	if m.width <= 0 {
		return line
	}
	return truncate.StringWithTail(line, uint(m.width), "…")
}

// renderStage renders one panel row. A running row leads with the animated
// spinner (live) or a plain bullet (static), then shows its bar/count/detail;
// a finished row shows ✔ + summary; a failed row shows ✗ + error.
func (m model) renderStage(s *stage, withSpinner bool) string {
	switch s.state {
	case stageDone:
		return completedLine(s.label, s.summary)
	case stageFailed:
		// Flatten the error to one line: a multi-line message would add physical
		// rows the panel's repaint math doesn't account for.
		msg := strings.ReplaceAll(fmt.Sprintf("%v", s.err), "\n", " ")
		return crossStyle.Render("✗") + " " + s.label + ": " + msg
	}
	lead := m.spinner.View()
	if !withSpinner {
		lead = "•"
	}
	switch {
	case s.hasFraction:
		// Determinate fraction bar (byte-driven download): the bar fill follows the
		// explicit fraction and the detail carries the human "14.2 MB / 23.1 MB" —
		// no N/M counter, since the unit is bytes, not steps.
		return fmt.Sprintf("%s %s %s  %s", lead, s.label, m.bar.ViewAs(s.fraction), s.detail)
	case s.total > 0:
		// Known total → an accurate bar (inventory/analysis, and the clone's
		// receiving-objects phase). Clamp: a count that overshoots total (a bad
		// SetTotal, or an extra Advance) must not drive the bar past 100%.
		pct := float64(s.count) / float64(s.total)
		if pct > 1 {
			pct = 1
		}
		return fmt.Sprintf("%s %s %s %d/%d  %s", lead, s.label, m.bar.ViewAs(pct), s.count, s.total, s.detail)
	case s.count > 0:
		// No upfront total (recon): a running counter keeps the row moving.
		return fmt.Sprintf("%s %s  %d  %s", lead, s.label, s.count, s.detail)
	case s.detail != "":
		// A status string with no count (clone connecting/writing, rules fetch).
		return fmt.Sprintf("%s %s  %s", lead, s.label, s.detail)
	default:
		return fmt.Sprintf("%s %s", lead, s.label)
	}
}

// TTYReporter forwards Reporter calls to a running bubbletea program. It
// implements Reporter and adds Run/Done to drive the render loop.
//
// Reporter methods do NOT call p.Send directly. bubbletea's msgs channel is
// unbuffered and has no reader until Run() starts the event loop, so a Send
// from the scan goroutine before Run() would block forever — and Run() is
// simultaneously blocking to start that loop, deadlocking both goroutines on
// the default interactive path. Instead methods enqueue onto a buffered channel
// that a pump goroutine (started by Run) forwards into p.Send once the loop is
// reading. Enqueue never blocks on the not-yet-running program, removing the
// startup race.
type TTYReporter struct {
	p    *tea.Program
	msgs chan tea.Msg
}

// NewTTY builds a TTYReporter rendering to w (stderr). The caller runs the loop
// with Run() on the main goroutine while emitting events from another goroutine.
func NewTTY(w io.Writer) *TTYReporter {
	p := tea.NewProgram(newModel(), tea.WithOutput(w))
	// The buffer only needs to hold events produced in the sliver of time between
	// the first Reporter call and Run() starting the loop; 1024 is ample headroom.
	return &TTYReporter{p: p, msgs: make(chan tea.Msg, 1024)}
}

// send enqueues a message for the render loop without ever blocking on the
// program-not-yet-running (the deadlock this guards against). The pump in Run
// forwards it. If the buffer ever fills (a flood before Run starts — practically
// impossible since Run is called immediately after spawning the worker), the
// caller blocks only until the pump drains, never indefinitely.
func (r *TTYReporter) send(m tea.Msg) { r.msgs <- m }

// Run renders until Done/Fatal triggers quit. Call on the main goroutine. A
// user interrupt (Ctrl-C) surfaces as ErrInterrupted so the caller can exit
// cleanly rather than printing bubbletea's raw kill error.
func (r *TTYReporter) Run() error {
	// Forward buffered events into the program once its loop is reading. Started
	// before p.Run() so it is ready the instant the loop comes up; p.Send blocks
	// only until then, then delivers in FIFO order. The pump stops when the
	// program ends (done closed); a pump left mid-Send after an interrupt is
	// harmless because the process is exiting.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case m := <-r.msgs:
				r.p.Send(m)
			case <-done:
				return
			}
		}
	}()
	_, err := r.p.Run()
	close(done)
	if err != nil {
		if errors.Is(err, tea.ErrProgramKilled) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}

// Done signals the render loop to stop (call after the job finishes).
func (r *TTYReporter) Done() { r.send(doneMsg{}) }

func (r *TTYReporter) StartPhase(key, label string) { r.send(startPhaseMsg{key, label}) }
func (r *TTYReporter) SetTotal(n int)               { r.send(setTotalMsg{n}) }
func (r *TTYReporter) Advance(detail string)        { r.send(advanceMsg{detail}) }
func (r *TTYReporter) SetDetail(detail string)      { r.send(setDetailMsg{detail}) }
func (r *TTYReporter) SetProgress(fraction float64, detail string) {
	r.send(setProgressMsg{fraction, detail})
}
func (r *TTYReporter) ResetPhase()             { r.send(resetPhaseMsg{}) }
func (r *TTYReporter) EndPhase(summary string) { r.send(endPhaseMsg{summary}) }
func (r *TTYReporter) Fatal(err error)         { r.send(fatalMsg{err}) }

var _ Reporter = (*TTYReporter)(nil)
