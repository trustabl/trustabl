package analysis_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverClaudeAgentOptions_CapturesPermissionMode(t *testing.T) {
	src := `from claude_agent_sdk import ClaudeAgentOptions, ClaudeSDKClient

async def main():
    options = ClaudeAgentOptions(
        system_prompt="hi",
        permission_mode="bypassPermissions",
    )
`
	opts := analysis.DiscoverClaudeAgentOptions([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(opts) != 1 {
		t.Fatalf("expected 1 ClaudeAgentOptions, got %d", len(opts))
	}
	if opts[0].Kwargs == nil || opts[0].Kwargs.Children["permission_mode"] == nil {
		t.Fatalf("permission_mode kwarg not captured: %+v", opts[0])
	}
	val := opts[0].Kwargs.Children["permission_mode"].Value
	if val == nil || val.Kind != models.ExprLiteralString {
		t.Fatalf("permission_mode value not a string literal: %+v", val)
	}
	if got := strings.Trim(val.Text, `"'`); got != "bypassPermissions" {
		t.Errorf("permission_mode = %q, want bypassPermissions", got)
	}
	if opts[0].FilePath != "main.py" {
		t.Errorf("FilePath = %q, want main.py", opts[0].FilePath)
	}
	if opts[0].Line == 0 {
		t.Errorf("expected a non-zero start line, got 0")
	}
}

func TestDiscoverClaudeAgentOptions_SilentWhenAbsent(t *testing.T) {
	src := `from agents import Agent

agent = Agent(name="x")
`
	opts := analysis.DiscoverClaudeAgentOptions([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(opts) != 0 {
		t.Errorf("expected 0 ClaudeAgentOptions, got %d", len(opts))
	}
}
