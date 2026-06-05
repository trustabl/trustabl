package progress

import (
	"io"
	"testing"
	"time"
)

// TestTTYReporter_SendDoesNotBlockBeforeRun guards the startup-deadlock fix: the
// Reporter methods must enqueue without blocking on the not-yet-running
// bubbletea program. Before the buffered-pump fix, the first method's direct
// p.Send blocked forever (bubbletea's msgs channel is unbuffered and has no
// reader until Run starts the loop), and because the CLI spawns the scan
// goroutine BEFORE calling Run on the main goroutine, both goroutines deadlocked
// on the default interactive path. Here Run is never called, so the old code
// would hang on the first method; the buffered send must return promptly.
func TestTTYReporter_SendDoesNotBlockBeforeRun(t *testing.T) {
	r := NewTTY(io.Discard)
	done := make(chan struct{})
	go func() {
		r.StartPhase("recon", "Recon")
		r.SetTotal(3)
		r.Advance("a.py")
		r.SetDetail("…")
		r.ResetPhase()
		r.EndPhase("done")
		r.Done()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Reporter methods blocked before Run() — startup deadlock regression")
	}
}
