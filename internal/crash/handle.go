package crash

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/trustabl/trustabl/internal/telemetry"
)

// Handle is the panic entry point: it captures a scrubbed report, always writes
// it to a local file, then (only on an interactive TTY) prompts the user. It
// never exits or flushes — the caller owns tel.Flush() and os.Exit(2).
func Handle(recovered any, stack []byte, meta Meta, tel *telemetry.Client) {
	rep := Capture(recovered, stack, meta)

	path := "(could not be written)"
	if dir, err := DefaultConfigDir(); err == nil {
		if p, werr := rep.WriteFile(dir, time.Now().UTC()); werr == nil {
			path = p
		}
	}
	fmt.Fprintf(os.Stderr, "\nTrustabl crashed. A crash report was saved to\n  %s\n", path)

	act(os.Stderr, os.Stdin, isInteractive(), rep, path, tel, openBrowser)
}

// act is the testable core: it runs the prompt and performs the chosen action.
func act(w io.Writer, r io.Reader, interactive bool, rep Report, path string, tel *telemetry.Client, opener func(string) error) {
	if !interactive {
		return
	}
	switch Prompt(w, r) {
	case ChoiceSend:
		if tel != nil {
			tel.TrackCrash(rep.Props())
		}
		fmt.Fprintln(w, "Thanks — report sent.")
	case ChoiceGitHub:
		u := IssueURL(rep)
		_ = opener(u)
		fmt.Fprintf(w, "File an issue here (paste %s):\n  %s\n", path, u)
	case ChoiceNothing:
	}
}

// isInteractive reports whether we may prompt: a TTY on stderr and not in CI.
func isInteractive() bool {
	if os.Getenv("CI") != "" || telemetry.DetectCIProvider() != "" {
		return false
	}
	return isatty.IsTerminal(os.Stderr.Fd())
}

// openBrowser best-effort opens a URL in the user's browser.
func openBrowser(u string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{u}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", u}
	default:
		cmd, args = "xdg-open", []string{u}
	}
	return exec.Command(cmd, args...).Start()
}
