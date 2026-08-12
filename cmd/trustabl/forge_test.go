package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestForgeCommand_NoPolicyFlag_NoArgs_OK(t *testing.T) {
	// --policy is optional; no args and no --policy should be accepted by cobra
	// (the SDK-detection error is a runtime concern, not a flag-parse error).
	cmd := newForgeCommand(nil)
	if cmd.Args == nil {
		t.Error("expected Args validator to be set")
	}
	// Verify that the --policy flag is NOT marked required.
	pf := cmd.Flags().Lookup("policy")
	if pf == nil {
		t.Fatal("--policy flag not registered")
	}
	if pf.Annotations != nil {
		if _, required := pf.Annotations["cobra_annotation_bash_completion_one_required_flag"]; required {
			t.Error("--policy should not be a required flag")
		}
	}
}

func TestForgeCommand_UnknownPolicy_Exit1(t *testing.T) {
	root := &cobra.Command{Use: "trustabl", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newForgeCommand(nil))
	root.SetArgs([]string{"forge", "--policy", "not_a_real_sdk"})

	buf := &bytes.Buffer{}
	root.SetErr(buf)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown --policy value")
	}
	// exitCodeError.Error() returns "", so check stderr for the bad value
	if !strings.Contains(buf.String(), "not_a_real_sdk") {
		t.Errorf("stderr should mention the bad policy value, got: %s", buf.String())
	}
}

func TestForgeCommand_Flags(t *testing.T) {
	cmd := newForgeCommand(nil)
	if cmd.Flags().Lookup("policy") == nil {
		t.Error("--policy flag not registered")
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Error("--output flag not registered")
	}
	if cmd.Flags().Lookup("rules-ref") == nil {
		t.Error("--rules-ref flag not registered")
	}
}

func TestForgeCommand_HelpText(t *testing.T) {
	cmd := newForgeCommand(nil)
	if !strings.Contains(cmd.Long, "SKILL.md") {
		t.Error("help text should mention SKILL.md")
	}
	if !strings.Contains(cmd.Long, "--policy") {
		t.Error("help text should mention --policy flag")
	}
	if !strings.Contains(cmd.Use, "[target]") {
		t.Errorf("Use should include [target], got: %s", cmd.Use)
	}
}

func TestForgeCommand_PolicyFlag_CommaSeparated(t *testing.T) {
	// Verify cobra can parse comma-separated --policy values without panicking.
	cmd := newForgeCommand(nil)
	buf := &bytes.Buffer{}
	cmd.SetErr(buf)
	// Providing two known categories — will fail at rules-resolve since no network,
	// but flag parsing itself must succeed (no unknown-category error).
	if err := cmd.Flags().Set("policy", "openai_sdk,mcp"); err != nil {
		t.Fatalf("failed to set --policy flag: %v", err)
	}
}

func TestForgeCommand_MaxOnePositionalArg(t *testing.T) {
	cmd := newForgeCommand(nil)
	// Two positional args should be rejected.
	buf := &bytes.Buffer{}
	cmd.SetErr(buf)
	cmd.SetOut(buf)
	err := cmd.Args(cmd, []string{"arg1", "arg2"})
	if err == nil {
		t.Error("expected error for two positional args")
	}
}
