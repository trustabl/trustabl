package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validRulePack = `policy:
  id: cs
  name: Claude SDK
  category: claude_sdk
  description: t
rules:
  - id: OK-1
    title: ok
    scope: tool
    severity: low
    confidence: 0.8
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`

// runRulesValidate invokes `trustabl rules validate <dir>` in-process and
// returns the command's error (nil on success), discarding its output.
func runRulesValidate(t *testing.T, dir string) error {
	t.Helper()
	cmd := newRulesCommand()
	cmd.SetArgs([]string{"validate", dir})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func writeRulePack(t *testing.T, dir, body string) {
	t.Helper()
	full := filepath.Join(dir, "claude_sdk", "pack.yaml")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRulesValidate covers the rules-repo CI gate: a well-formed pack passes,
// and a schema violation fails (non-nil error → non-zero exit).
func TestRulesValidate(t *testing.T) {
	t.Run("valid packs pass", func(t *testing.T) {
		dir := t.TempDir()
		writeRulePack(t, dir, validRulePack)
		if err := runRulesValidate(t, dir); err != nil {
			t.Fatalf("valid rules should validate, got %v", err)
		}
	})

	t.Run("malformed rule fails", func(t *testing.T) {
		dir := t.TempDir()
		writeRulePack(t, dir, strings.Replace(validRulePack, "severity: low", "severity: nonsense", 1))
		if err := runRulesValidate(t, dir); err == nil {
			t.Fatal("a malformed rule must fail validation")
		}
	})

	t.Run("unknown predicate fails", func(t *testing.T) {
		dir := t.TempDir()
		writeRulePack(t, dir, strings.Replace(validRulePack, "has_docstring: true", "has_quantum_flux: true", 1))
		if err := runRulesValidate(t, dir); err == nil {
			t.Fatal("an unknown predicate must fail strict validation")
		}
	})
}
