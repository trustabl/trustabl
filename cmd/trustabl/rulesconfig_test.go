package main

import "testing"

// TestEffectiveRules covers the source-selection precedence that drives BOTH the
// rulesource.Config and the RulesOrigin, so the resolved source and its reported
// provenance can never disagree. The default is still git this phase; the flip to
// production is a one-line change to defaultRulesSource (ENG-6).
func TestEffectiveRules(t *testing.T) {
	// Neutralize any ambient override so cases are hermetic.
	t.Setenv("TRUSTABL_RULES_REPO", "")

	t.Run("default is unsigned git", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "" {
			t.Errorf("Channel = %q, want git path (empty)", cfg.Channel)
		}
		if origin.Signed || origin.Custom {
			t.Errorf("origin = %+v, want unsigned default", origin)
		}
	})

	t.Run("--rules-source git is the explicit unsigned default", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesSource: "git"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "" || origin.Signed || origin.Custom {
			t.Errorf("cfg=%+v origin=%+v, want unsigned official git", cfg, origin)
		}
	})

	t.Run("--rules-source production selects the signed channel", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesSource: "production"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "production" {
			t.Errorf("Channel = %q, want production", cfg.Channel)
		}
		if !origin.Signed || origin.Channel != "production" {
			t.Errorf("origin = %+v, want signed production", origin)
		}
	})

	t.Run("--channel is a deprecated alias for a signed channel", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{channel: "staging"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "staging" || !origin.Signed || origin.Channel != "staging" {
			t.Errorf("cfg=%+v origin=%+v, want signed staging", cfg, origin)
		}
	})

	t.Run("--rules-repo alone implies git and is watermarked custom", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesRepo: "https://example.com/r"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "" {
			t.Errorf("Channel = %q, want git", cfg.Channel)
		}
		if cfg.RepoURL != "https://example.com/r" {
			t.Errorf("RepoURL = %q", cfg.RepoURL)
		}
		if origin.Signed || !origin.Custom {
			t.Errorf("origin = %+v, want unsigned custom", origin)
		}
	})

	t.Run("--rules-ref alone pins a git ref on the official repo (not custom)", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesRef: "v0.1.0"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "" || cfg.Ref != "v0.1.0" {
			t.Errorf("cfg = %+v, want git pinned to v0.1.0", cfg)
		}
		if origin.Signed || origin.Custom {
			t.Errorf("origin = %+v, want unsigned default (official repo, pinned ref)", origin)
		}
	})

	t.Run("signed channel from a custom repo is flagged custom (signed-fork)", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesSource: "production", rulesRepo: "https://example.com/fork"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "production" || cfg.RepoURL != "https://example.com/fork" {
			t.Errorf("cfg = %+v, want signed production from the fork", cfg)
		}
		// A signed channel from a non-official repo must be marked Custom so it is
		// watermarked and gets a distinct ScanID — not reported as official production.
		if !origin.Signed || !origin.Custom {
			t.Errorf("origin = %+v, want signed AND custom", origin)
		}
		if origin.Watermark() == "" {
			t.Error("signed custom-repo scan must carry a watermark")
		}
	})

	t.Run("signed production from the official repo is clean (not custom)", func(t *testing.T) {
		cfg, origin, err := effectiveRules(scanFlags{rulesSource: "production"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "production" || cfg.RepoURL != "" {
			t.Errorf("cfg = %+v, want official production", cfg)
		}
		if !origin.Signed || origin.Custom {
			t.Errorf("origin = %+v, want signed and NOT custom", origin)
		}
		if origin.Watermark() != "" {
			t.Errorf("official production must be watermark-clean, got %q", origin.Watermark())
		}
	})

	t.Run("--rules-ref with a signed channel is an error", func(t *testing.T) {
		if _, _, err := effectiveRules(scanFlags{rulesSource: "production", rulesRef: "v1"}); err == nil {
			t.Fatal("expected error: --rules-ref has no meaning on a signed channel")
		}
	})

	t.Run("conflicting --rules-source and --channel is an error", func(t *testing.T) {
		if _, _, err := effectiveRules(scanFlags{rulesSource: "production", channel: "staging"}); err == nil {
			t.Fatal("expected error on conflicting source/channel")
		}
	})

	t.Run("env TRUSTABL_RULES_REPO forces unsigned custom git", func(t *testing.T) {
		t.Setenv("TRUSTABL_RULES_REPO", "https://example.com/env")
		cfg, origin, err := effectiveRules(scanFlags{})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Channel != "" || cfg.RepoURL != "https://example.com/env" || !origin.Custom {
			t.Errorf("cfg=%+v origin=%+v, want unsigned custom from env", cfg, origin)
		}
	})

	t.Run("NoUpdate passes through", func(t *testing.T) {
		cfg, _, err := effectiveRules(scanFlags{noRulesUpdate: true})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.NoUpdate {
			t.Error("NoUpdate not propagated")
		}
	})
}
