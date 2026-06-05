package enrichment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// mockLLM records calls and returns preset results.
type mockLLM struct {
	results []enrichResult
	err     error
	calls   int
}

func (m *mockLLM) enrichFile(_ context.Context, _ string, issues []issueContext) ([]enrichResult, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	// return one result per issue
	out := make([]enrichResult, len(issues))
	for i := range issues {
		if i < len(m.results) {
			out[i] = m.results[i]
		}
	}
	return out, nil
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

func TestPipeline_BasicEnrichment(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		ScanID:       "scan-001",
		Repo:         "myrepo",
		RulesVersion: "abc123",
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", Line: 2, Severity: "high", Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		llm: &mockLLM{results: []enrichResult{
			{Explanation: "AI explanation", Fix: "Add guardrail", LineStart: 2, LineEnd: 2, Replacement: "    agent = Agent(input_guardrails=[g])"},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(enriched.Findings) != 1 {
		t.Fatalf("len(Findings) = %d, want 1", len(enriched.Findings))
	}
	f := enriched.Findings[0]
	if !f.Enriched {
		t.Error("Enriched = false, want true")
	}
	if f.AIExplanation != "AI explanation" {
		t.Errorf("AIExplanation = %q, want AI explanation", f.AIExplanation)
	}
	if enriched.ScanID != "scan-001" {
		t.Errorf("ScanID = %q, want scan-001", enriched.ScanID)
	}
	if enriched.EnrichedAt == 0 {
		t.Error("EnrichedAt should be non-zero")
	}
}

func TestPipeline_OnlyEnriched_FiltersUnenriched(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", Line: 2, Title: "A"},
			{RuleID: "CSDK-011", FilePath: "no_file.py", Line: 1, Title: "B"}, // file missing → not enriched
		},
	}

	p := &Pipeline{
		RepoRoot:     dir,
		OnlyEnriched: true,
		llm: &mockLLM{results: []enrichResult{
			{Explanation: "AI explanation", Fix: "fix"},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(enriched.Findings) != 1 {
		t.Errorf("len(Findings) = %d, want 1 (only the enriched one)", len(enriched.Findings))
	}
}

func TestPipeline_RuleFilter(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", Line: 2, Title: "A"},
			{RuleID: "CSDK-011", FilePath: "agent.py", Line: 2, Title: "B"},
		},
	}

	mock := &mockLLM{results: []enrichResult{
		{Explanation: "Only CSDK-010 was enriched"},
	}}

	p := &Pipeline{
		RepoRoot:   dir,
		RuleFilter: []string{"CSDK-010"},
		llm:        mock,
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1 (only one file)", mock.calls)
	}
	// CSDK-010 is enriched; CSDK-011 passes through unenriched
	var enrichedCount int
	for _, f := range enriched.Findings {
		if f.Enriched {
			enrichedCount++
		}
	}
	if enrichedCount != 1 {
		t.Errorf("enriched count = %d, want 1", enrichedCount)
	}
}

func TestPipeline_FindingsGroupedByFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "a.py", "def run():\n    x = 1\n    y = 2\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "R1", FilePath: "a.py", Line: 2, Title: "F1"},
			{RuleID: "R2", FilePath: "a.py", Line: 3, Title: "F2"},
		},
	}

	mock := &mockLLM{results: []enrichResult{
		{Explanation: "e1"}, {Explanation: "e2"},
	}}

	p := &Pipeline{RepoRoot: dir, llm: mock}
	_, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// Both findings are in the same file → exactly one LLM call
	if mock.calls != 1 {
		t.Errorf("LLM calls = %d, want 1 (batched by file)", mock.calls)
	}
}

func TestApplyPatches_SinglePatch(t *testing.T) {
	content := "line1\nline2\nline3\nline4\n"
	patches := []filePatch{{lineStart: 2, lineEnd: 2, replacement: "NEW_LINE2"}}
	got, err := applyPatches(content, patches)
	if err != nil {
		t.Fatalf("applyPatches error: %v", err)
	}
	if !strings.Contains(got, "NEW_LINE2") {
		t.Error("replacement not applied")
	}
	if strings.Contains(got, "line2") {
		t.Error("old line2 should be replaced")
	}
}

func TestApplyPatches_MultiPatch_DescendingOrder(t *testing.T) {
	// Two patches on different lines — must apply in descending order to keep indices stable
	content := "a\nb\nc\nd\n"
	patches := []filePatch{
		{lineStart: 1, lineEnd: 1, replacement: "A"},
		{lineStart: 3, lineEnd: 3, replacement: "C"},
	}
	got, err := applyPatches(content, patches)
	if err != nil {
		t.Fatalf("applyPatches error: %v", err)
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "C") {
		t.Errorf("not all patches applied: %q", got)
	}
	if strings.Contains(got, "\na\n") || strings.Contains(got, "\nc\n") {
		t.Errorf("old lines still present: %q", got)
	}
}

func TestApplyPatches_OutOfBounds(t *testing.T) {
	content := "line1\nline2\n"
	patches := []filePatch{{lineStart: 99, lineEnd: 99, replacement: "x"}}
	_, err := applyPatches(content, patches)
	if err == nil {
		t.Error("expected error for out-of-bounds patch")
	}
}

func TestPipeline_Apply_WritesToDisk(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", Line: 2, Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		Apply:    true,
		llm: &mockLLM{results: []enrichResult{
			{
				Explanation: "Missing guardrail",
				Fix:         "Add guardrails",
				LineStart:   2,
				LineEnd:     2,
				Replacement: "    agent = Agent(input_guardrails=[g])",
			},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Applied=true is set on the finding that had a replacement
	if len(enriched.Findings) != 1 {
		t.Fatalf("len(Findings) = %d, want 1", len(enriched.Findings))
	}
	if !enriched.Findings[0].Applied {
		t.Error("Applied = false, want true after --apply with a replacement")
	}

	// Verify the file on disk was actually rewritten
	content, err := os.ReadFile(filepath.Join(dir, "agent.py"))
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if !strings.Contains(string(content), "input_guardrails") {
		t.Errorf("patched file does not contain replacement; got:\n%s", content)
	}
}

func TestPipeline_Apply_SkipsFalsePositive(t *testing.T) {
	dir := t.TempDir()
	original := "def run():\n    agent = Agent()\n    return agent\n"
	writeTempFile(t, dir, "agent.py", original)

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", Line: 2, Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		Apply:    true,
		llm: &mockLLM{results: []enrichResult{
			{
				Explanation:   "Actually fine",
				Fix:           "No fix needed",
				LineStart:     2,
				LineEnd:       2,
				Replacement:   "    agent = Agent(input_guardrails=[g])",
				FalsePositive: true, // LLM says skip this one
			},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Applied must be false — FalsePositive suppresses the write
	if enriched.Findings[0].Applied {
		t.Error("Applied = true, want false when FalsePositive=true")
	}

	// File on disk must be unchanged
	content, err := os.ReadFile(filepath.Join(dir, "agent.py"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != original {
		t.Errorf("file was modified despite FalsePositive=true; got:\n%s", content)
	}
}

func TestPipeline_MultiFile_BatchedByFile(t *testing.T) {
	dir := t.TempDir()
	// Create 4 different files
	for _, name := range []string{"a.py", "b.py", "c.py", "d.py"} {
		writeTempFile(t, dir, name, "def run():\n    x = 1\n")
	}

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "R1", FilePath: "a.py", Line: 2, Title: "F1"},
			{RuleID: "R2", FilePath: "b.py", Line: 2, Title: "F2"},
			{RuleID: "R3", FilePath: "c.py", Line: 2, Title: "F3"},
			{RuleID: "R4", FilePath: "d.py", Line: 2, Title: "F4"},
			// Two findings in a.py — should still be one LLM call for a.py
			{RuleID: "R5", FilePath: "a.py", Line: 1, Title: "F5"},
		},
	}

	mock := &mockLLM{results: []enrichResult{
		{Explanation: "e1"}, {Explanation: "e2"},
	}}

	p := &Pipeline{RepoRoot: dir, llm: mock}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// 5 findings in, 5 findings out
	if len(enriched.Findings) != 5 {
		t.Errorf("len(Findings) = %d, want 5", len(enriched.Findings))
	}

	// 4 unique files → 4 LLM calls (a.py has 2 findings but 1 call)
	if mock.calls != 4 {
		t.Errorf("LLM calls = %d, want 4 (one per unique file)", mock.calls)
	}
}
