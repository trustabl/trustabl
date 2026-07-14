package main

import (
	"errors"
	"testing"
)

func TestCategorizeScanError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		// nil
		{"", ""},
		// rules fetch
		{"fetch rules: connection refused", "rules_fetch_failed"},
		{"resolve rules: timeout", "rules_fetch_failed"},
		{"rulesource: clone failed", "rules_fetch_failed"},
		// clone
		{"git clone failed: repo not found", "clone_failed"},
		{"cloning repository: 403 forbidden", "clone_failed"},
		// no rules
		{"no rules found in pack", "no_rules"},
		{"no usable rules after filtering", "no_rules"},
		{"no compatible rules for schema version", "no_rules"},
		{"no rules in pack", "no_rules"},
		{"all rules incompatible with this engine", "no_rules"},
		// parse error
		{"parse error at line 42", "parse_error"},
		{"ast error: unexpected token", "parse_error"},
		{"tree-sitter failed", "parse_error"},
		{"syntax error in file.py", "parse_error"},
		{"failed to parse imports", "parse_error"},
		// unknown
		{"unexpected nil pointer", "unknown"},
		{"disk full", "unknown"},
	}

	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = errors.New(tc.msg)
		}
		got := categorizeScanError(err)
		if got != tc.want {
			t.Errorf("categorizeScanError(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestFailurePhase(t *testing.T) {
	cases := []struct {
		category, want string
	}{
		{"rules_fetch_failed", "rules"},
		{"no_rules", "rules"},
		{"clone_failed", "clone"},
		{"parse_error", "inventory"},
		{"unknown", "unknown"},
		{"anything_else", "unknown"},
	}

	for _, tc := range cases {
		got := failurePhase(tc.category)
		if got != tc.want {
			t.Errorf("failurePhase(%q) = %q, want %q", tc.category, got, tc.want)
		}
	}
}
