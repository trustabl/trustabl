package crash

import (
	"bufio"
	"io"
	"strings"
)

// Choice is the user's response to the crash prompt.
type Choice int

const (
	ChoiceNothing Choice = iota
	ChoiceSend
	ChoiceGitHub
)

// Prompt asks the user what to do with a crash report and returns their choice.
// All three options are always shown — crash reporting is independent of the
// telemetry setting, so every crash offers the same choice. Empty input, a
// second invalid input, or EOF all yield ChoiceNothing — the default is always
// silence. Re-prompts once on the first invalid input.
func Prompt(w io.Writer, r io.Reader) Choice {
	io.WriteString(w, "\nHelp us fix it? No source code or file contents are sent.\n"+
		"  1. Send anonymous crash report\n"+
		"  2. Open GitHub issue\n"+
		"  3. Do nothing\n\n"+
		"Enter 1, 2, or 3 [default: 3]: ")

	scanner := bufio.NewScanner(r)
	for attempt := 0; attempt < 2; attempt++ {
		if !scanner.Scan() {
			return ChoiceNothing
		}
		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			return ChoiceSend
		case "2":
			return ChoiceGitHub
		case "", "3":
			return ChoiceNothing
		}
		if attempt == 0 {
			io.WriteString(w, "Please try again: ")
		}
	}
	return ChoiceNothing
}
