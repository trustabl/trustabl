package progress

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The panel renders every stage at once: finished stages stay as ✔ rows while
// the active stage shows its bar — a stacked live panel, not one line.
func TestModelPanelRendersAllStagesTogether(t *testing.T) {
	var m tea.Model = newModel()
	step := func(msg tea.Msg) { m, _ = m.Update(msg) }
	step(startPhaseMsg{key: "rules", label: "Resolving rules"})
	step(endPhaseMsg{summary: "a270e59"})
	step(startPhaseMsg{key: "inventory", label: "Inventory"})
	step(setTotalMsg{n: 773})
	step(advanceMsg{detail: "agent_loop.py"})

	v := m.(model).View()
	lines := strings.Split(v, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 panel rows, got %d: %q", len(lines), v)
	}
	if !strings.Contains(lines[0], "✔") || !strings.Contains(lines[0], "Resolving rules") {
		t.Errorf("row 0 should be the finished rules stage, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Inventory") || !strings.Contains(lines[1], "1/773") {
		t.Errorf("row 1 should be the active inventory bar, got %q", lines[1])
	}
}

// endPhaseMsg marks the active stage done (kept in the panel as a ✔ row) and
// clears the active index — it no longer prints to scrollback per-phase.
func TestModelEndPhaseMarksStageDone(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "recon", label: "Recon"})
	m3, _ := m2.(model).Update(endPhaseMsg{summary: "18 files"})
	mm := m3.(model)
	if mm.cur != -1 {
		t.Errorf("cur should be -1 after endPhase, got %d", mm.cur)
	}
	if len(mm.stages) != 1 || mm.stages[0].state != stageDone || mm.stages[0].summary != "18 files" {
		t.Fatalf("stage not marked done: %+v", mm.stages)
	}
	if v := mm.View(); !strings.Contains(v, "Recon") || !strings.Contains(v, "18 files") {
		t.Errorf("view should render the done row, got %q", v)
	}
}

// doneMsg flips the model to quitting (live region clears) and returns a
// command that prints the persistent panel then quits.
func TestModelDoneQuits(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "recon", label: "Recon"})
	m3, _ := m2.(model).Update(endPhaseMsg{summary: "x"})
	m4, cmd := m3.(model).Update(doneMsg{})
	mm := m4.(model)
	if !mm.quitting {
		t.Error("model should be quitting after doneMsg")
	}
	if cmd == nil {
		t.Fatal("doneMsg should return a command (println + quit)")
	}
	if mm.View() != "" {
		t.Errorf("view should be empty while quitting, got %q", mm.View())
	}
}

// A phase with no known total (e.g. recon, where the walk discovers the file
// count) must still show its running count and current detail — otherwise the
// live line is a bare spinner during the phase's slow span.
func TestModelViewShowsCountAndDetailWithoutTotal(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "recon", label: "Recon"})
	m3, _ := m2.(model).Update(advanceMsg{detail: "src/agents/loop.py"})
	view := m3.(model).View()
	if !strings.Contains(view, "1") {
		t.Errorf("view should show running count 1, got %q", view)
	}
	if !strings.Contains(view, "src/agents/loop.py") {
		t.Errorf("view should show current detail, got %q", view)
	}
}

// setDetailMsg sets a live detail line with no count/bar (clone sub-phase,
// network status). View shows "spinner label detail" — never a fill bar that
// could read as complete while work continues.
func TestModelSetDetailShowsSpinnerLineNoBar(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "clone", label: "Cloning"})
	m3, _ := m2.(model).Update(setDetailMsg{detail: "Receiving objects 45%"})
	mm := m3.(model)
	if s := mm.current(); s == nil || s.detail != "Receiving objects 45%" {
		t.Errorf("after setDetail: current stage detail wrong: %+v", s)
	}
	v := mm.View()
	if !strings.Contains(v, "Cloning") || !strings.Contains(v, "Receiving objects 45%") {
		t.Errorf("view should show label and detail, got %q", v)
	}
	if strings.Contains(v, "/") {
		t.Errorf("detail-only phase must not render a count/total bar, got %q", v)
	}
}

// completedLine renders a finished stage: a check mark, the stage
// label, and its summary — but suppresses the summary when it's already in the
// label (the clone label carries the URL, so it must not be doubled).
func TestCompletedLine(t *testing.T) {
	got := completedLine("Recon", "773 files · python")
	if !strings.Contains(got, "✔") || !strings.Contains(got, "Recon") || !strings.Contains(got, "773 files · python") {
		t.Errorf("completedLine(Recon) = %q, missing check/label/summary", got)
	}

	// Empty summary → just check + label, no trailing separator.
	bare := completedLine("Resolving rules", "")
	if !strings.Contains(bare, "Resolving rules") || strings.Contains(bare, "  ") {
		t.Errorf("completedLine with empty summary = %q, want '✔ Resolving rules' with no double-space", bare)
	}

	// Clone: label already contains the URL → summary (same URL) must not repeat.
	url := "https://github.com/octocat/Spoon-Knife"
	clone := completedLine("Cloning "+url, url)
	if strings.Count(clone, url) != 1 {
		t.Errorf("completedLine duplicated the URL: %q", clone)
	}
}

// fatalMsg marks the active stage failed (a ✗ row carrying the error), flips to
// quitting, and returns a command (println the final panel, then quit). This is
// the path a user sees when a clone or rules fetch fails.
func TestModelFatalRendersFailedRow(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "clone", label: "Cloning"})
	m3, cmd := m2.(model).Update(fatalMsg{err: errors.New("dial tcp: connection refused")})
	mm := m3.(model)
	if !mm.quitting {
		t.Error("fatal should set quitting")
	}
	if cmd == nil {
		t.Fatal("fatalMsg should return a command (println + quit)")
	}
	if mm.cur != -1 {
		t.Errorf("cur should be -1 after fatal, got %d", mm.cur)
	}
	if s := mm.stages[0]; s.state != stageFailed {
		t.Fatalf("stage should be failed, got state %d", s.state)
	}
	sv := mm.staticView()
	if !strings.Contains(sv, "✗") || !strings.Contains(sv, "Cloning") || !strings.Contains(sv, "dial tcp: connection refused") {
		t.Errorf("staticView should show ✗ + label + error, got %q", sv)
	}
}

// A long detail (a deep file path) on a narrow terminal must be truncated, not
// wrapped: a wrapped row corrupts the in-place panel repaint. Width arrives via
// WindowSizeMsg; every rendered row must then fit within it.
func TestModelTruncatesRowsToWidth(t *testing.T) {
	m := newModel()
	steps := []tea.Msg{
		tea.WindowSizeMsg{Width: 24, Height: 40},
		startPhaseMsg{key: "inventory", label: "Inventory"},
		setTotalMsg{n: 100},
		advanceMsg{detail: strings.Repeat("deep/nested/path/", 12) + "agent_loop.py"},
	}
	var mm tea.Model = m
	for _, msg := range steps {
		mm, _ = mm.Update(msg)
	}
	for _, line := range strings.Split(mm.(model).View(), "\n") {
		if w := lipgloss.Width(line); w > 24 {
			t.Errorf("row width %d exceeds terminal width 24: %q", w, line)
		}
	}
}

// With no WindowSizeMsg (width 0, the unit-test default), rows are not
// truncated — width 0 means "unknown", and truncating to 0 would erase the row.
func TestModelNoTruncationBeforeWindowSize(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "inventory", label: "Inventory"})
	m3, _ := m2.(model).Update(advanceMsg{detail: "a/reasonably/long/path/agent_loop.py"})
	if v := m3.(model).View(); !strings.Contains(v, "agent_loop.py") {
		t.Errorf("width-0 view should not truncate detail, got %q", v)
	}
}

// resetPhaseMsg drops a counted phase back to a bare spinner: after a partial
// bar (total+count set), a reset followed by a detail must render the detail
// line, NOT a stale frozen bar — the clone's fetch→PlainClone fallback path.
func TestModelResetPhaseClearsBar(t *testing.T) {
	m := newModel()
	steps := []tea.Msg{
		startPhaseMsg{key: "clone", label: "Cloning"},
		setTotalMsg{n: 100},
		advanceMsg{detail: "receiving objects"}, // bar now at 1/100
		resetPhaseMsg{},
		setDetailMsg{detail: "cloning…"},
	}
	var mm tea.Model = m
	for _, msg := range steps {
		mm, _ = mm.Update(msg)
	}
	s := mm.(model).current()
	if s.total != 0 || s.count != 0 {
		t.Errorf("reset should zero total/count, got total=%d count=%d", s.total, s.count)
	}
	v := mm.(model).View()
	if !strings.Contains(v, "cloning…") {
		t.Errorf("view should show the post-reset detail, got %q", v)
	}
	if strings.Contains(v, "/100") {
		t.Errorf("view must not show the stale 1/100 bar after reset, got %q", v)
	}
}

func TestModelAdvanceTracksCount(t *testing.T) {
	m := newModel()
	m2, _ := m.Update(startPhaseMsg{key: "inventory", label: "Inventory"})
	m3, _ := m2.(model).Update(setTotalMsg{n: 3})
	m4, _ := m3.(model).Update(advanceMsg{detail: "a.py"})
	s := m4.(model).current()
	if s == nil || s.count != 1 || s.total != 3 || s.detail != "a.py" {
		t.Errorf("after advance: stage=%+v", s)
	}
}
