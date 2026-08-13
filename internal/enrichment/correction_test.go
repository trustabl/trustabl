package enrichment

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestPipeline_InvalidReplacement_CorrectedOnRetry: the LLM's first replacement
// doesn't parse; one reviseResult call fixes it. The corrected CODE replaces
// the broken one, but the original explanation/fix survive untouched — the
// correction call's only job is fixing the syntax, not re-explaining the finding.
func TestPipeline_InvalidReplacement_CorrectedOnRetry(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
		},
	}

	mock := &mockLLM{
		results: []enrichResult{
			{Explanation: "bad", Fix: "add a guardrail", LineStart: 2, LineEnd: 2, Replacement: "    agent = Agent(input_guardrails=[g]"}, // missing ')' — invalid
		},
		reviseResults: []string{
			"    agent = Agent(input_guardrails=[g])", // just the corrected code
		},
	}

	p := &Pipeline{RepoRoot: dir, llm: mock}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if mock.reviseCalls.Load() != 1 {
		t.Errorf("reviseCalls = %d, want 1", mock.reviseCalls.Load())
	}
	f := enriched.Findings[0]
	if f.Replacement != "    agent = Agent(input_guardrails=[g])" {
		t.Errorf("Replacement = %q, want the corrected replacement", f.Replacement)
	}
	if f.AIExplanation != "bad" {
		t.Errorf("AIExplanation = %q, want the ORIGINAL explanation preserved (correction only fixes code)", f.AIExplanation)
	}
	if f.AIFix != "add a guardrail" {
		t.Errorf("AIFix = %q, want the ORIGINAL fix text preserved", f.AIFix)
	}
}

// TestPipeline_ExhaustedCorrections: every attempt (original + 1 correction)
// stays invalid. The finding must fall back to the "no code change" shape —
// Replacement/LineStart/LineEnd cleared, but Explanation preserved — rather
// than ever surface unparseable code.
func TestPipeline_ExhaustedCorrections(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "agent.py", "def run():\n    agent = Agent()\n    return agent\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "CSDK-010", FilePath: "agent.py", StartLine: 2, EndLine: 2, Title: "No guardrail"},
		},
	}

	mock := &mockLLM{
		results: []enrichResult{
			{Explanation: "bad", LineStart: 2, LineEnd: 2, Replacement: "    agent = Agent(input_guardrails=[g]"}, // invalid
		},
		reviseResults: []string{
			"    agent = Agent(input_guardrails=[g]", // still invalid
		},
	}

	p := &Pipeline{RepoRoot: dir, llm: mock}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if mock.reviseCalls.Load() != 1 {
		t.Errorf("reviseCalls = %d, want 1 (maxCorrectionAttempts-1, not the whole batch re-run)", mock.reviseCalls.Load())
	}
	f := enriched.Findings[0]
	if f.Replacement != "" {
		t.Errorf("Replacement = %q, want empty after exhausting corrections", f.Replacement)
	}
	if f.LineStart != 0 || f.LineEnd != 0 {
		t.Errorf("LineStart/LineEnd = %d/%d, want 0/0 after exhausting corrections", f.LineStart, f.LineEnd)
	}
	if f.AIExplanation != "bad" {
		t.Errorf("AIExplanation = %q, want the original explanation preserved even when the replacement is dropped", f.AIExplanation)
	}
}

// TestPipeline_BatchIsolation: 3 findings in one file, only the middle one's
// replacement is invalid. Correction must be scoped to that single finding —
// the other two must never trigger a reviseResult call.
func TestPipeline_BatchIsolation(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "vals.py", "def run():\n    a = 1\n    b = 2\n    c = 3\n    return a, b, c\n")

	result := &models.ScanResult{
		Findings: []models.Finding{
			{RuleID: "R1", FilePath: "vals.py", StartLine: 2, EndLine: 2, Title: "F1"},
			{RuleID: "R2", FilePath: "vals.py", StartLine: 3, EndLine: 3, Title: "F2"},
			{RuleID: "R3", FilePath: "vals.py", StartLine: 4, EndLine: 4, Title: "F3"},
		},
	}

	mock := &mockLLM{
		results: []enrichResult{
			{Explanation: "e1", LineStart: 2, LineEnd: 2, Replacement: "    a = 10"},  // valid
			{Explanation: "e2", LineStart: 3, LineEnd: 3, Replacement: "    b = (20"}, // invalid — missing ')'
			{Explanation: "e3", LineStart: 4, LineEnd: 4, Replacement: "    c = 30"},  // valid
		},
		reviseResults: []string{
			"    b = 20",
		},
	}

	p := &Pipeline{RepoRoot: dir, llm: mock}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if mock.reviseCalls.Load() != 1 {
		t.Errorf("reviseCalls = %d, want 1 (only the invalid finding, not the valid ones)", mock.reviseCalls.Load())
	}

	byRule := map[string]models.EnrichedFinding{}
	for _, f := range enriched.Findings {
		byRule[f.RuleID] = f
	}
	if byRule["R1"].Replacement != "    a = 10" {
		t.Errorf("R1 Replacement = %q, want untouched original", byRule["R1"].Replacement)
	}
	if byRule["R2"].Replacement != "    b = 20" {
		t.Errorf("R2 Replacement = %q, want the corrected replacement", byRule["R2"].Replacement)
	}
	if byRule["R2"].AIExplanation != "e2" {
		t.Errorf("R2 AIExplanation = %q, want the original e2 preserved", byRule["R2"].AIExplanation)
	}
	if byRule["R3"].Replacement != "    c = 30" {
		t.Errorf("R3 Replacement = %q, want untouched original", byRule["R3"].Replacement)
	}
}
