package enrichment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// captureLLM records the issueContexts it was called with.
type captureLLM struct {
	mu     sync.Mutex
	issues []issueContext
	err    error
}

func (m *captureLLM) enrichFile(_ context.Context, _ string, issues []issueContext) ([]enrichResult, error) {
	m.mu.Lock()
	m.issues = append(m.issues, issues...)
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make([]enrichResult, len(issues))
	for i := range out {
		out[i] = enrichResult{Explanation: "ok"}
	}
	return out, nil
}

// fakeTraces returns canned stats and records which tools were looked up.
type fakeTraces struct {
	mu    sync.Mutex
	asked []string
	stats map[string]*models.ToolTraceStats
	err   error
}

func (f *fakeTraces) ToolStats(_ context.Context, toolName string) (*models.ToolTraceStats, error) {
	f.mu.Lock()
	f.asked = append(f.asked, toolName)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.stats[toolName], nil
}

func TestPipeline_TraceEvidenceOnToolFindings(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "tools.py", "def fetch_data():\n    pass\n\ndef main():\n    pass\n")

	traces := &fakeTraces{stats: map[string]*models.ToolTraceStats{
		"fetch_data": {
			ToolName: "fetch_data", Project: "prod", Runs: 25, Errors: 6,
			AvgLatencyMS: 3211, RecentErrors: []string{"Timeout after 30s"},
		},
	}}
	llm := &captureLLM{}
	p := &Pipeline{RepoRoot: dir, Traces: traces, llm: llm}

	result := &models.ScanResult{Findings: []models.Finding{
		{RuleID: "CSDK-010", Scope: models.ScopeTool, ToolName: "fetch_data", FilePath: "tools.py", StartLine: 1, Severity: "high"},
		{RuleID: "CSDK-120", Scope: models.ScopeAgent, ToolName: "my_agent", FilePath: "tools.py", StartLine: 4, Severity: "medium"},
	}}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Tool finding carries the evidence; agent finding does not.
	toolTE := enriched.Findings[0].TraceEvidence
	if !strings.Contains(toolTE, "25 run(s)") || !strings.Contains(toolTE, "6 error(s)") || !strings.Contains(toolTE, "Timeout after 30s") {
		t.Errorf("tool TraceEvidence = %q, want runs/errors/recent-error summary", toolTE)
	}
	if enriched.Findings[1].TraceEvidence != "" {
		t.Errorf("agent TraceEvidence = %q, want empty (tool scope only)", enriched.Findings[1].TraceEvidence)
	}
	// Lookup was gated to the tool-scope finding only.
	if len(traces.asked) != 1 || traces.asked[0] != "fetch_data" {
		t.Errorf("trace lookups = %v, want [fetch_data] only", traces.asked)
	}
	// The evidence reached the LLM prompt context.
	var sawEvidence bool
	for _, iss := range llm.issues {
		if iss.ruleID == "CSDK-010" && strings.Contains(iss.traceEvidence, "6 error(s)") {
			sawEvidence = true
		}
		if iss.ruleID == "CSDK-120" && iss.traceEvidence != "" {
			t.Errorf("agent issueContext.traceEvidence = %q, want empty", iss.traceEvidence)
		}
	}
	if !sawEvidence {
		t.Error("tool issueContext never carried trace evidence into the LLM call")
	}
}

func TestPipeline_TraceErrorDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "tools.py", "def fetch_data():\n    pass\n")

	p := &Pipeline{
		RepoRoot: dir,
		Traces:   &fakeTraces{err: errors.New("langsmith: HTTP 500")},
		llm:      &captureLLM{},
	}
	result := &models.ScanResult{Findings: []models.Finding{
		{RuleID: "CSDK-010", Scope: models.ScopeTool, ToolName: "fetch_data", FilePath: "tools.py", StartLine: 1},
	}}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run: %v (trace errors must never fail the run)", err)
	}
	f := enriched.Findings[0]
	if !f.Enriched {
		t.Error("Enriched = false, want true: LLM enrichment must proceed without trace evidence")
	}
	if f.TraceEvidence != "" {
		t.Errorf("TraceEvidence = %q, want empty on lookup error", f.TraceEvidence)
	}
}

func TestPipeline_NoTracesForToolIsSilent(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "tools.py", "def fetch_data():\n    pass\n")

	p := &Pipeline{
		RepoRoot: dir,
		Traces:   &fakeTraces{stats: map[string]*models.ToolTraceStats{}}, // nil for every tool
		llm:      &captureLLM{},
	}
	result := &models.ScanResult{Findings: []models.Finding{
		{RuleID: "CSDK-010", Scope: models.ScopeTool, ToolName: "fetch_data", FilePath: "tools.py", StartLine: 1},
	}}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if enriched.Findings[0].TraceEvidence != "" {
		t.Errorf("TraceEvidence = %q, want empty when tool has no trace history", enriched.Findings[0].TraceEvidence)
	}
}

func TestPipeline_TraceEvidenceSurvivesLLMFailure(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "tools.py", "def fetch_data():\n    pass\n")

	traces := &fakeTraces{stats: map[string]*models.ToolTraceStats{
		"fetch_data": {ToolName: "fetch_data", Project: "prod", Runs: 10, Errors: 10},
	}}
	p := &Pipeline{
		RepoRoot: dir,
		Traces:   traces,
		llm:      &captureLLM{err: errors.New("llm unreachable")},
	}
	result := &models.ScanResult{Findings: []models.Finding{
		{RuleID: "CSDK-010", Scope: models.ScopeTool, ToolName: "fetch_data", FilePath: "tools.py", StartLine: 1},
	}}
	enriched, err := p.Run(context.Background(), result)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	f := enriched.Findings[0]
	if f.Enriched {
		t.Error("Enriched = true, want false (LLM failed)")
	}
	// The observed runtime data is independent of the LLM and must survive.
	if !strings.Contains(f.TraceEvidence, "10 error(s)") {
		t.Errorf("TraceEvidence = %q, want it preserved despite LLM failure", f.TraceEvidence)
	}
}

func TestFormatTraceEvidence(t *testing.T) {
	got := formatTraceEvidence(&models.ToolTraceStats{
		ToolName: "fetch_data", Project: "prod", Runs: 25, Errors: 6,
		AvgLatencyMS: 3211, RecentErrors: []string{"Timeout after 30s", "KeyError 'url'"},
	})
	for _, want := range []string{`project "prod"`, "last 25 run(s)", `tool "fetch_data"`, "6 error(s)", "(24% error rate)", "avg latency 3211ms", "Timeout after 30s | KeyError 'url'"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatTraceEvidence = %q, missing %q", got, want)
		}
	}

	clean := formatTraceEvidence(&models.ToolTraceStats{ToolName: "t", Project: "p", Runs: 8})
	if !strings.Contains(clean, "0 error(s) (0% error rate)") || strings.Contains(clean, "Recent errors") {
		t.Errorf("clean-tool evidence = %q, want zero-error summary with no error list", clean)
	}
}
