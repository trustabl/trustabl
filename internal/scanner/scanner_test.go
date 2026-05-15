package scanner_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/karenctl/internal/scanner"
)

// TestScanSampleAgent is the end-to-end smoke test: scan the example repo and
// assert that the rules we expect to fire actually fire.
//
// This is NOT the detection-quality benchmark called out in architecture §8.
// That's a corpus-level eval. This is the one-repo, one-known-set-of-bugs
// floor that catches regressions in plumbing.
func TestScanSampleAgent(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repo := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "sample_agent")

	result, err := scanner.Run(scanner.Config{Target: repo})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(result.Tools) == 0 {
		t.Fatalf("expected at least one discovered tool, got 0")
	}

	// Every rule we ship should fire at least once on this fixture.
	expectedRules := []string{
		"CSDK-001", "CSDK-002", "CSDK-003", "CSDK-004", "CSDK-005",
		"CSDK-006", "CSDK-007",
		"OSH-001", "OSH-002", "OSH-003", "OSH-004", "OSH-005",
	}
	seen := map[string]bool{}
	for _, f := range result.Findings {
		seen[f.RuleID] = true
	}
	for _, rule := range expectedRules {
		if !seen[rule] {
			t.Errorf("expected rule %s to fire on sample_agent, did not", rule)
		}
	}

	// Generation: both files should be present.
	wantFiles := []string{
		"hooks/pretooluse_validate.py",
		"hooks/posttooluse_log.py",
		"openshell/policy.yaml",
	}
	gotFiles := map[string]bool{}
	for _, a := range result.GeneratedArtifacts {
		gotFiles[a.RelativePath] = true
	}
	for _, p := range wantFiles {
		if !gotFiles[p] {
			t.Errorf("expected generated artifact %s, missing", p)
		}
	}

	// Determinism: a second scan must produce byte-identical artifacts.
	result2, err := scanner.Run(scanner.Config{Target: repo})
	if err != nil {
		t.Fatalf("second scan failed: %v", err)
	}
	for i, a := range result.GeneratedArtifacts {
		if a.Contents != result2.GeneratedArtifacts[i].Contents {
			t.Errorf("artifact %s is non-deterministic between scans", a.RelativePath)
		}
	}
}
