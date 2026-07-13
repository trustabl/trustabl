package crash

import (
	"strings"
	"testing"
)

func TestPromptWithSend(t *testing.T) {
	cases := map[string]struct {
		input string
		want  Choice
	}{
		"send":              {"1\n", ChoiceSend},
		"github":            {"2\n", ChoiceGitHub},
		"nothing":           {"3\n", ChoiceNothing},
		"empty":             {"\n", ChoiceNothing},
		"eof":               {"", ChoiceNothing},
		"invalid twice":     {"x\ny\n", ChoiceNothing},
		"invalid then send": {"x\n1\n", ChoiceSend},
	}
	for name, c := range cases {
		var out strings.Builder
		got := Prompt(&out, strings.NewReader(c.input))
		if got != c.want {
			t.Errorf("%s: Prompt(%q) = %v, want %v", name, c.input, got, c.want)
		}
	}
}

func TestPromptAlwaysShowsAllOptions(t *testing.T) {
	// Crash reporting is independent of telemetry — every crash shows all three
	// options, including Send, regardless of any telemetry state.
	var out strings.Builder
	Prompt(&out, strings.NewReader("\n"))
	menu := out.String()
	for _, want := range []string{"1. Send crash report", "2. Open GitHub issue", "3. Do nothing", "[default: 3]"} {
		if !strings.Contains(menu, want) {
			t.Errorf("prompt missing %q, got:\n%s", want, menu)
		}
	}
}
