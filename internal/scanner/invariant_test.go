package scanner_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestInvariant_DegradedNeverSilent is the executable form of the rules-
// distribution-v2 accepted invariant (ENG-16): an old binary handed a pack it
// only partly understands must ALWAYS either produce a real scan — at least one
// genuine rule actually fired — or refuse loudly with an error and no report.
// An empty-findings ScanResult caused by a failure to load rules must be
// unreachable.
//
// Three legs, run as one named contract so CI fails if any single leg
// regresses:
//
//	Leg 1 (degraded, but loud): a pack mixing one evaluable rule with one
//	    forward-incompatible rule scans successfully, fires the evaluable rule,
//	    AND surfaces the skip as a META-005 finding + a RulesSkipped entry.
//	Leg 2 (all-incompatible → refuse): a pack whose every rule needs a newer
//	    engine yields ErrAllRulesIncompatible and a zero-value ScanResult — no
//	    report is produced.
//	Leg 3 (empty pack → refuse): a schema-compatible pack carrying zero rules
//	    yields ErrNoRulesInPack and a zero-value ScanResult.
//
// Legs 2 and 3 are the two — and only two — ways rule loading can fail to yield
// a usable ruleset, and both fail closed. That is what makes "a silent empty
// report is impossible" a property and not a hope: there is no third path where
// the ruleset vanishes yet Run still returns a populated ScanResult.
//
// Per-mechanism coverage already exists (LoadFor-level refusals in
// internal/rules/loadfor_test.go; the single-rule META-005 emission in
// scanner_test.go). This test asserts the whole invariant end-to-end through
// scanner.Run, the boundary a caller actually sees.
func TestInvariant_DegradedNeverSilent(t *testing.T) {
	// claudeSDKTarget writes a minimal repo that discovery classifies as using
	// the Claude Agent SDK, so LoadFor selects the claude_sdk pack. One tool
	// definition is enough for a tool-scoped rule to have something to fire on.
	claudeSDKTarget := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		write := func(rel, content string) {
			full := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
		write("package.json", `{"dependencies": {"@anthropic-ai/claude-agent-sdk": "^1.0.0"}}`)
		write("src/agent.ts", `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
export const searchTool = tool("search", "Search", { q: z.string() }, async () => ({ content: [] }));
`)
		return dir
	}

	// Leg 1 — degraded, but loud. INV-FIRE matches `always: true` (a known no-op
	// predicate) so it fires on every claude_sdk_tool; INV-SKIP uses an unknown
	// `skill` scope a future engine could evaluate but this build cannot, so the
	// lenient loader drops it. The scan must succeed, the evaluable rule must
	// actually fire, and the drop must be visible (META-005 + RulesSkipped).
	//
	// INV-FIRE sets `language: typescript` deliberately: the loader defaults a
	// missing `language:` to python, and the tool-scope Applies gate rejects a
	// python rule against this typescript tool — so an unset language would make
	// INV-FIRE silently never apply and quietly defeat the "real rule fired"
	// assertion this leg exists to make.
	t.Run("degraded scan fires real rules and surfaces the skip", func(t *testing.T) {
		rulesFS := fstest.MapFS{
			"claude_sdk/pack.yaml": &fstest.MapFile{Data: []byte(`
policy:
  id: cs
  name: Claude SDK
  category: claude_sdk
  description: t
rules:
  - id: INV-FIRE
    title: Always fires
    scope: tool
    severity: low
    confidence: 0.8
    language: typescript
    applies_to: [claude_sdk_tool]
    match:
      always: true
    explanation: x
    fix: y
  - id: INV-SKIP
    title: Needs a newer engine
    scope: skill
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
    match:
      has_docstring: true
    explanation: x
    fix: y
`)},
		}

		res, err := scanner.Run(scanner.Config{Target: claudeSDKTarget(t), RulesFS: rulesFS})
		if err != nil {
			t.Fatalf("Run on a partly-compatible pack must succeed, got: %v", err)
		}

		// A genuine, non-META rule actually fired. This is the heart of the
		// invariant: "valid but degraded" still means real rules ran, never an
		// empty report dressed up as a clean bill of health.
		var firedRealRule bool
		for _, f := range res.Findings {
			if !strings.HasPrefix(f.RuleID, "META-") {
				firedRealRule = true
				break
			}
		}
		if !firedRealRule {
			t.Fatalf("no real rule fired — degraded scan produced only META findings: %+v", res.Findings)
		}

		// The drop is loud: recorded on RulesSkipped...
		if !contains(res.RulesSkipped, "INV-SKIP") {
			t.Errorf("RulesSkipped must contain INV-SKIP, got %v", res.RulesSkipped)
		}
		// ...and surfaced in the report body as a single META-005 naming it.
		var meta005 []models.Finding
		for _, f := range res.Findings {
			if f.RuleID == "META-005" {
				meta005 = append(meta005, f)
			}
		}
		if len(meta005) != 1 {
			t.Fatalf("want exactly one META-005, got %d: %+v", len(meta005), meta005)
		}
		if !strings.Contains(meta005[0].Explanation, "INV-SKIP") {
			t.Errorf("META-005 must name the skipped rule, got: %q", meta005[0].Explanation)
		}
	})

	// Leg 2 — every rule needs a newer engine. INV-INCOMPAT uses an unknown
	// predicate, so the lenient loader drops it and the pack has no evaluable
	// rules left. Run must refuse with ErrAllRulesIncompatible and emit no report.
	t.Run("all-incompatible pack refuses with no report", func(t *testing.T) {
		rulesFS := fstest.MapFS{
			"claude_sdk/pack.yaml": &fstest.MapFile{Data: []byte(`
policy:
  id: cs
  name: Claude SDK
  category: claude_sdk
  description: t
rules:
  - id: INV-INCOMPAT
    title: Unknown predicate
    scope: tool
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
    match:
      has_quantum_flux: true
    explanation: x
    fix: y
`)},
		}

		res, err := scanner.Run(scanner.Config{Target: claudeSDKTarget(t), RulesFS: rulesFS})
		if !errors.Is(err, rules.ErrAllRulesIncompatible) {
			t.Fatalf("want ErrAllRulesIncompatible, got err=%v", err)
		}
		assertNoReport(t, res)
	})

	// Leg 3 — schema-compatible pack with zero rules. Run must refuse with
	// ErrNoRulesInPack and emit no report. "The engine never runs rule-less."
	t.Run("empty pack refuses with no report", func(t *testing.T) {
		rulesFS := fstest.MapFS{
			"claude_sdk/pack.yaml": &fstest.MapFile{Data: []byte(`
policy:
  id: cs
  name: Claude SDK
  category: claude_sdk
  description: t
rules: []
`)},
		}

		res, err := scanner.Run(scanner.Config{Target: claudeSDKTarget(t), RulesFS: rulesFS})
		if !errors.Is(err, rules.ErrNoRulesInPack) {
			t.Fatalf("want ErrNoRulesInPack, got err=%v", err)
		}
		assertNoReport(t, res)
	})
}

// assertNoReport fails if a refused scan leaked a non-zero ScanResult. The
// refusal legs must return the zero value: no ScanID, no findings, no
// inventory — the engine produced nothing a caller could mistake for a report.
func assertNoReport(t *testing.T, res models.ScanResult) {
	t.Helper()
	if res.ScanID != "" {
		t.Errorf("refused scan leaked a ScanID: %q", res.ScanID)
	}
	if len(res.Findings) != 0 {
		t.Errorf("refused scan leaked %d findings: %+v", len(res.Findings), res.Findings)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
