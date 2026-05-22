package scanner_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanExamples_NoCrash sweeps every immediate subdirectory under
// examples/ and asserts the scanner completes without error.
//
// This is NOT a correctness test — it does NOT assert specific findings,
// because the examples are real-world agents (or close to it) and shouldn't
// reliably trigger every rule. The point is regression coverage: if
// discovery starts panicking on weird code shapes, this catches it before
// the scanner ships broken to a real user.
//
// Per-rule fire/silent correctness lives in
// internal/rules/policies_test.go, which uses focused snippets.
func TestScanExamples_NoCrash(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	examplesDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples")

	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Skipf("no examples/ dir at %s: %v", examplesDir, err)
	}

	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip very large dataset directories that are not real agents and
		// would slow the test pointlessly. Add to this list as needed.
		switch e.Name() {
		case "ToolBench":
			continue
		}

		target := filepath.Join(examplesDir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			result, err := scanner.Run(scanner.Config{Target: target})
			if err != nil {
				t.Fatalf("scan %s: %v", e.Name(), err)
			}
			// Manifest must always be populated — even an empty repo gets
			// a RepoRoot. A nil manifest is a sign of a wiring break.
			if result.Manifest.RepoRoot == "" {
				t.Errorf("scan %s: empty RepoRoot in manifest", e.Name())
			}
		})
		scanned++
	}
	if scanned == 0 {
		t.Skip("examples/ has no scannable subdirectories")
	}
}

func TestScan_SurfacesNewInventoryFields(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "financial_research_agent")
	res, err := scanner.Run(scanner.Config{Target: target})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	foundWebSearch := false
	for _, h := range res.HostedTools {
		if h.Class == "WebSearchTool" {
			foundWebSearch = true
		}
	}
	if !foundWebSearch {
		t.Errorf("expected WebSearchTool in ScanResult.HostedTools, got %+v", res.HostedTools)
	}
}
