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

// TestScanDeterministic_LocationFields is a belt-and-suspenders assertion that
// every entity's Location (FilePath, Line, EndLine) is identical across two
// consecutive runs. The existing TestScanDeterministic covers this indirectly
// via ScanID equality; this test makes the Location contract explicit so a
// future change that breaks Location determinism (e.g. by introducing map
// iteration order in discovery) fails this specifically named test.
func TestScanDeterministic_LocationFields(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "deterministic-fixture")

	cfg := scanner.Config{Target: fixture, RulesFS: rulesFixtureFS(t), RulesVersion: "fixedsha"}
	r1, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	r2, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	if len(r1.Agents) != len(r2.Agents) {
		t.Fatalf("agent count differs: %d vs %d", len(r1.Agents), len(r2.Agents))
	}
	for i := range r1.Agents {
		a1, a2 := r1.Agents[i], r2.Agents[i]
		if a1.Location != a2.Location {
			t.Errorf("agents[%d].Location differs: %+v vs %+v", i, a1.Location, a2.Location)
		}
	}

	if len(r1.HostedTools) != len(r2.HostedTools) {
		t.Fatalf("hosted tool count differs: %d vs %d", len(r1.HostedTools), len(r2.HostedTools))
	}
	for i := range r1.HostedTools {
		h1, h2 := r1.HostedTools[i], r2.HostedTools[i]
		if h1.Location != h2.Location {
			t.Errorf("hosted_tools[%d].Location differs: %+v vs %+v", i, h1.Location, h2.Location)
		}
	}

	if len(r1.MCPServers) != len(r2.MCPServers) {
		t.Fatalf("mcp server count differs: %d vs %d", len(r1.MCPServers), len(r2.MCPServers))
	}
	for i := range r1.MCPServers {
		m1, m2 := r1.MCPServers[i], r2.MCPServers[i]
		if m1.Location != m2.Location {
			t.Errorf("mcp_servers[%d].Location differs: %+v vs %+v", i, m1.Location, m2.Location)
		}
	}

	if len(r1.Subagents) != len(r2.Subagents) {
		t.Fatalf("subagent count differs: %d vs %d", len(r1.Subagents), len(r2.Subagents))
	}
	for i := range r1.Subagents {
		if r1.Subagents[i].Location != r2.Subagents[i].Location {
			t.Errorf("subagents[%d].Location differs: %+v vs %+v", i, r1.Subagents[i].Location, r2.Subagents[i].Location)
		}
	}

	if len(r1.ClaudeSettings) != len(r2.ClaudeSettings) {
		t.Fatalf("claude_settings count differs: %d vs %d", len(r1.ClaudeSettings), len(r2.ClaudeSettings))
	}
	for i := range r1.ClaudeSettings {
		if r1.ClaudeSettings[i].Location != r2.ClaudeSettings[i].Location {
			t.Errorf("claude_settings[%d].Location differs: %+v vs %+v", i, r1.ClaudeSettings[i].Location, r2.ClaudeSettings[i].Location)
		}
	}
}

// TestScanDeterministic_TSFixture asserts that two runs over the same TS fixture
// with the same rules version produce the same ScanID. Guards the determinism
// contract in ARCHITECTURE.md §7 for TypeScript scans.
func TestScanDeterministic_TSFixture(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "deterministic-ts-fixture")

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
		t.Errorf("non-deterministic ScanID: %q vs %q", r1.ScanID, r2.ScanID)
	}
}
