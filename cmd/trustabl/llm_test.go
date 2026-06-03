package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/llm"
)

// setLLMConfigDir overrides the LLM config directory for the duration of the test.
func setLLMConfigDir(t *testing.T, dir string) {
	t.Helper()
	old := llm.ConfigDir
	llm.ConfigDir = dir
	t.Cleanup(func() { llm.ConfigDir = old })
}

func TestLLMList_NoConfig(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No LLM configuration found") {
		t.Errorf("got %q, want output containing 'No LLM configuration found'", buf.String())
	}
}

func TestLLMList_WithConfig(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetKey("sk-ant-api03-testkey12345678901234")
	if err := cfg.Save(); err != nil {
		t.Fatalf("setup Save() error: %v", err)
	}

	cmd := newLLMListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "anthropic") {
		t.Errorf("output %q missing 'anthropic'", out)
	}
	if !strings.Contains(out, "****...") {
		t.Errorf("output %q missing masked key pattern '****...'", out)
	}
}

func TestLLMKeyGet_NoKey(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMKeyGetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No API key configured") {
		t.Errorf("got %q, want output containing 'No API key configured'", buf.String())
	}
}

func TestLLMKeyGet_WithKey(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetKey("sk-ant-api03-testkey12345678901234")
	if err := cfg.Save(); err != nil {
		t.Fatalf("setup Save() error: %v", err)
	}

	cmd := newLLMKeyGetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "****...1234" {
		t.Errorf("got %q, want ****...1234", got)
	}
}

func TestLLMList_ActiveMarker(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetKey("sk-ant-api03-testkey12345678901234")
	if err := cfg.Save(); err != nil {
		t.Fatalf("setup Save() error: %v", err)
	}

	cmd := newLLMListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "anthropic *") {
		t.Errorf("output %q missing active-provider marker 'anthropic *'", out)
	}
}

func TestLLMKeySet_ValidArg(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMKeySetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "API key for anthropic saved") {
		t.Errorf("got %q, want confirmation message", buf.String())
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ActiveProvider().Key != "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA" {
		t.Errorf("Key not persisted, got %q", cfg.ActiveProvider().Key)
	}
}

func TestLLMKeySet_InvalidKey(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantMsg string
	}{
		{"wrong prefix", "not-a-valid-key", "invalid API key format"},
		{"empty arg", "", "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setLLMConfigDir(t, t.TempDir())

			cmd := newLLMKeySetCommand()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{tt.arg})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for key %q, got nil", tt.arg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestLLMKeySet_NonInteractive(t *testing.T) {
	// When no arg is supplied and stdin is not a terminal (always true in go test),
	// the command must return an actionable error rather than hanging.
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMKeySetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when stdin is not a terminal, got nil")
	}
	if !strings.Contains(err.Error(), "stdin is not a terminal") {
		t.Errorf("error %q does not mention 'stdin is not a terminal'", err.Error())
	}
}

func TestLLMKeyDelete_Confirm(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetKey("sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA")
	if err := cfg.Save(); err != nil {
		t.Fatalf("setup Save() error: %v", err)
	}

	cmd := newLLMKeyDeleteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "deleted") {
		t.Errorf("got %q, want confirmation of deletion", buf.String())
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() after delete error: %v", err)
	}
	if cfg.ActiveProvider().Key != "" {
		t.Errorf("key still set after delete: %q", cfg.ActiveProvider().Key)
	}
}

func TestLLMKeyDelete_Abort(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetKey("sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAA")
	if err := cfg.Save(); err != nil {
		t.Fatalf("setup Save() error: %v", err)
	}

	for _, input := range []string{"N\n", "n\n", "\n"} {
		t.Run("input="+strings.TrimSpace(input), func(t *testing.T) {
			cmd := newLLMKeyDeleteCommand()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetIn(strings.NewReader(input))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(buf.String(), "Aborted") {
				t.Errorf("got %q, want 'Aborted'", buf.String())
			}

			cfg2, err := llm.Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg2.ActiveProvider().Key == "" {
				t.Error("key was deleted despite abort")
			}
		})
	}
}

func TestLLMKeyDelete_NoKey(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMKeyDeleteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No API key configured") {
		t.Errorf("got %q, want 'No API key configured'", buf.String())
	}
}

func TestLLMModelSet(t *testing.T) {
	setLLMConfigDir(t, t.TempDir())

	cmd := newLLMModelSetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"claude-opus-4-7"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "claude-opus-4-7") {
		t.Errorf("output %q missing model name", buf.String())
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ActiveProvider().Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", cfg.ActiveProvider().Model)
	}
}
