package enrichment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// mockLLM records calls and returns preset results.
type mockLLM struct {
	results []enrichResult
	err     error
	calls   atomic.Int32
}

func (m *mockLLM) enrichFile(_ context.Context, _ string, issues []issueContext) ([]enrichResult, error) {
	m.calls.Add(1)
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
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Severity: "high", Title: "No guardrail"},
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
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "A"},
			{RuleID: "CSDK-011", FilePath: "no_file.py", StartLine: 1, EndLine: 1, Title: "B"}, // file missing → not enriched
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
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "A"},
			{RuleID: "CSDK-011", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "B"},
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
	if mock.calls.Load() != 1 {
		t.Errorf("LLM calls = %d, want 1 (only one file)", mock.calls.Load())
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
			{RuleID: "R1", FilePath: "a.py", StartLine: 2, EndLine: 2, Title: "F1"},
			{RuleID: "R2", FilePath: "a.py", StartLine: 3, EndLine: 3, Title: "F2"},
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
	if mock.calls.Load() != 1 {
		t.Errorf("LLM calls = %d, want 1 (batched by file)", mock.calls.Load())
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
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
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
				Original:    "    agent = Agent()",
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
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
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

func TestUnifiedDiff_BasicReplacement(t *testing.T) {
	allLines := []string{"def run():", "    agent = Agent()", "    return agent"}
	result := unifiedDiff("agent.py", allLines, 2, 2, "    agent = Agent(input_guardrails=[g])")
	if !strings.Contains(result, "--- a/agent.py") {
		t.Errorf("missing --- header: %q", result)
	}
	if !strings.Contains(result, "+++ b/agent.py") {
		t.Errorf("missing +++ header: %q", result)
	}
	if !strings.Contains(result, "-    agent = Agent()") {
		t.Errorf("missing - line: %q", result)
	}
	if !strings.Contains(result, "+    agent = Agent(input_guardrails=[g])") {
		t.Errorf("missing + line: %q", result)
	}
}

func TestUnifiedDiff_NoChange(t *testing.T) {
	allLines := []string{"def run():", "    agent = Agent()", "    return agent"}
	result := unifiedDiff("agent.py", allLines, 2, 2, "    agent = Agent()")
	if result != "" {
		t.Errorf("expected empty string for identical replacement, got: %q", result)
	}
}

func TestUnifiedDiff_MultiLineReplacement(t *testing.T) {
	allLines := []string{"a", "b", "c", "d", "e"}
	// Replace lines 2-3 ("b","c") with 3 lines ("x","y","z")
	result := unifiedDiff("file.py", allLines, 2, 3, "x\ny\nz")
	if !strings.Contains(result, "--- a/file.py") {
		t.Errorf("missing --- header: %q", result)
	}
	// orig: 1 ctx + 2 orig + 2 ctx = 5; new: 1 ctx + 3 repl + 2 ctx = 6
	if !strings.Contains(result, "-1,5") {
		t.Errorf("orig count wrong in hunk header: %q", result)
	}
	if !strings.Contains(result, "+1,6") {
		t.Errorf("new count wrong in hunk header: %q", result)
	}
	if !strings.Contains(result, "-b") || !strings.Contains(result, "-c") {
		t.Errorf("missing - lines for original content: %q", result)
	}
	if !strings.Contains(result, "+x") || !strings.Contains(result, "+y") || !strings.Contains(result, "+z") {
		t.Errorf("missing + lines for replacement: %q", result)
	}
}

func TestUnifiedDiff_LineStartOne(t *testing.T) {
	// lineStart=1: no context lines before, should not panic
	allLines := []string{"line1", "line2", "line3"}
	result := unifiedDiff("f.py", allLines, 1, 1, "replaced")
	if !strings.Contains(result, "-line1") {
		t.Errorf("missing - line: %q", result)
	}
	if !strings.Contains(result, "+replaced") {
		t.Errorf("missing + line: %q", result)
	}
}

func TestUnifiedDiff_LineEndAtBoundary(t *testing.T) {
	// lineEnd == len(allLines): no context lines after, should not panic
	allLines := []string{"a", "b", "c"}
	result := unifiedDiff("f.py", allLines, 3, 3, "replaced")
	if !strings.Contains(result, "-c") {
		t.Errorf("missing - line: %q", result)
	}
	if !strings.Contains(result, "+replaced") {
		t.Errorf("missing + line: %q", result)
	}
}

func TestUnifiedDiff_OutOfBounds(t *testing.T) {
	allLines := []string{"a", "b"}
	// lineEnd > len(allLines) — LLM hallucinated line number; must return "" not panic
	result := unifiedDiff("f.py", allLines, 1, 99, "x")
	if result != "" {
		t.Errorf("expected empty string for out-of-bounds range, got: %q", result)
	}
	// lineStart > len(allLines)
	result = unifiedDiff("f.py", allLines, 99, 99, "x")
	if result != "" {
		t.Errorf("expected empty string for out-of-bounds lineStart, got: %q", result)
	}
}

func TestPipeline_DiffPopulated(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		Diff:     true,
		llm: &mockLLM{results: []enrichResult{
			{Explanation: "missing guardrail", Fix: "add guardrail", LineStart: 2, LineEnd: 2, Replacement: "    agent = Agent(input_guardrails=[g])"},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	diff := enriched.Findings[0].Diff
	if diff == "" {
		t.Error("Diff = empty, want non-empty")
	}
	if !strings.Contains(diff, "---") || !strings.Contains(diff, "+++") {
		t.Errorf("Diff missing headers: %q", diff)
	}
}

func TestPipeline_DiffFalse_NoDiffField(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		// Diff defaults to false
		llm: &mockLLM{results: []enrichResult{
			{Explanation: "missing guardrail", Fix: "add guardrail", LineStart: 2, LineEnd: 2, Replacement: "    agent = Agent(input_guardrails=[g])"},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if enriched.Findings[0].Diff != "" {
		t.Errorf("Diff = %q, want empty when Diff=false", enriched.Findings[0].Diff)
	}
}

func TestPipeline_DiffWithApply(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
		},
	}

	p := &Pipeline{
		RepoRoot: dir,
		Diff:     true,
		Apply:    true,
		llm: &mockLLM{results: []enrichResult{
			{Explanation: "missing guardrail", Fix: "add guardrail", LineStart: 2, LineEnd: 2, Original: "    agent = Agent()", Replacement: "    agent = Agent(input_guardrails=[g])"},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Patch must be written to disk
	if !enriched.Findings[0].Applied {
		t.Error("Applied = false, want true")
	}
	content, err := os.ReadFile(filepath.Join(dir, "agent.py"))
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if !strings.Contains(string(content), "input_guardrails") {
		t.Errorf("patched file does not contain replacement; got:\n%s", content)
	}

	// Diff must still be populated
	if enriched.Findings[0].Diff == "" {
		t.Error("Diff = empty, want non-empty when Diff=true and Apply=true")
	}
}

// TestPipeline_Apply_SkipsOnContentAnchorMismatch is the keystone data-safety
// test: when the file on disk no longer matches the `original` the model echoed
// (e.g. it was edited since the scan), the patch must be skipped, the file left
// byte-for-byte unchanged, and no backup written — never a wrong-line overwrite.
func TestPipeline_Apply_SkipsOnContentAnchorMismatch(t *testing.T) {
	dir := t.TempDir()
	// Line 2 on disk differs from what the model echoes as `original` below.
	onDisk := "def run():\n    agent = SomethingElse()\n    return agent\n"
	writeTempFile(t, dir, "agent.py", onDisk)

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
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
				Original:    "    agent = Agent()", // what the model saw — does NOT match the current file
				Replacement: "    agent = Agent(input_guardrails=[g])",
			},
		}},
	}

	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if enriched.Findings[0].Applied {
		t.Error("Applied = true, want false when the file no longer matches the model's echoed original")
	}
	content, err := os.ReadFile(filepath.Join(dir, "agent.py"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != onDisk {
		t.Errorf("file must be left byte-for-byte unchanged on anchor mismatch; got:\n%s", content)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "agent.py.trustabl.bak")); statErr == nil {
		t.Error("no backup should be written when nothing is applied")
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
			{RuleID: "R1", FilePath: "a.py", StartLine: 2, EndLine: 2, Title: "F1"},
			{RuleID: "R2", FilePath: "b.py", StartLine: 2, EndLine: 2, Title: "F2"},
			{RuleID: "R3", FilePath: "c.py", StartLine: 2, EndLine: 2, Title: "F3"},
			{RuleID: "R4", FilePath: "d.py", StartLine: 2, EndLine: 2, Title: "F4"},
			// Two findings in a.py — should still be one LLM call for a.py
			{RuleID: "R5", FilePath: "a.py", StartLine: 1, EndLine: 1, Title: "F5"},
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
	if mock.calls.Load() != 4 {
		t.Errorf("LLM calls = %d, want 4 (one per unique file)", mock.calls.Load())
	}
}
