package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScan_FlatCollectionSubagentFiresCSDK110 is the regression that motivated
// the whole markdown-agent-surface effort: a subagent-collection repo (VoltAgent
// layout) stores subagents under categories/<NN>/*.md, NOT .claude/agents/, and
// declares no Claude SDK code. Before this work the scanner reported it as
// 0 subagents / 0 findings (a silent false "clean"). It must now discover the
// subagent (shape fallback) AND fire CSDK-110 on the granted Bash tool (the
// policy-selection decoupling — subagent presence loads the claude_sdk pack).
func TestScan_FlatCollectionSubagentFiresCSDK110(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "categories/01-core/api-designer.md"),
		"---\nname: api-designer\ndescription: Designs APIs\ntools: Read, Write, Bash\nmodel: sonnet\n---\n\nYou design APIs.\n")

	res, err := scanner.Run(scanner.Config{
		Target:       dir,
		RulesFS:      rulesFixture(t),
		RulesVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Subagents) != 1 {
		t.Fatalf("subagents = %d, want 1 (flat-collection shape fallback)", len(res.Subagents))
	}
	var fired bool
	var ids []string
	for _, f := range res.Findings {
		ids = append(ids, f.RuleID)
		if f.RuleID == "CSDK-110" {
			fired = true
		}
	}
	if !fired {
		t.Errorf("CSDK-110 did not fire on a flat-collection subagent granting Bash; findings=%v", ids)
	}
}

func mustWrite(t *testing.T, full, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
