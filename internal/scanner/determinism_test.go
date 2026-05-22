package scanner_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanDeterministic asserts that two runs over the same fixture produce
// the same ScanID. Guards the contract documented in ARCHITECTURE.md §7.
func TestScanDeterministic(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "deterministic-fixture")

	r1, err := scanner.Run(scanner.Config{Target: fixture})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	r2, err := scanner.Run(scanner.Config{Target: fixture})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if r1.ScanID != r2.ScanID {
		t.Errorf("ScanID drifted: %q vs %q", r1.ScanID, r2.ScanID)
	}
}
