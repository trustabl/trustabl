package scanner_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// rulesFixtureFS returns the Phase-1 interim rule packs for tests.
func rulesFixtureFS(t *testing.T) fs.FS {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "rules-fixture")
	return os.DirFS(root)
}

// TestScanDeterministic asserts that two runs over the same fixture with the
// same rules version produce the same ScanID, and that changing the rules
// version changes the ScanID. Guards the contract in ARCHITECTURE.md §7.
func TestScanDeterministic(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "deterministic-fixture")

	cfg := scanner.Config{Target: fixture, RulesFS: rulesFixtureFS(t), RulesVersion: "fixedsha"}
	r1, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	r2, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if r1.ScanID != r2.ScanID {
		t.Errorf("ScanID drifted: %q vs %q", r1.ScanID, r2.ScanID)
	}

	// A different rules version must change the ScanID.
	cfg2 := cfg
	cfg2.RulesVersion = "differentsha"
	r3, err := scanner.Run(cfg2)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if r3.ScanID == r1.ScanID {
		t.Error("ScanID unchanged when rules version changed")
	}
}
