package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/sarif"
)

// TestFinishScan_GenericErrorNotDoublePrinted guards the double-print fix: in an
// active progress mode (plain/tty) the reporter's Fatal already presented the
// error, so finishScan must return a silent exitCodeError rather than the raw
// error (which main would print a second time). In off mode nothing presented
// it, so the raw error must propagate for main to print once.
func TestFinishScan_GenericErrorNotDoublePrinted(t *testing.T) {
	genErr := errors.New("boom")

	// Plain mode (human format, non-tty in tests): reporter already showed it.
	errPlain := finishScan(models.ScanResult{}, genErr, scanFlags{format: "human"})
	var ec exitCodeError
	if !errors.As(errPlain, &ec) {
		t.Errorf("plain mode: got %v, want silent exitCodeError", errPlain)
	}

	// Off mode (json format forces progress off): main must print it, so the
	// raw error propagates.
	errOff := finishScan(models.ScanResult{}, genErr, scanFlags{format: "json"})
	if !errors.Is(errOff, genErr) {
		t.Errorf("off mode: got %v, want the raw error to propagate", errOff)
	}
}

func TestVersionCommandOutput(t *testing.T) {
	// Save and restore the package-level vars so the test is hermetic.
	origV, origC, origD := version, commit, date
	defer func() { version, commit, date = origV, origC, origD }()

	version, commit, date = "1.2.3", "abc1234", "2026-05-26"

	cmd := newVersionCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.Run(cmd, nil)

	out := buf.String()
	for _, want := range []string{"1.2.3", "abc1234", "2026-05-26"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output %q missing %q", out, want)
		}
	}
}

func TestParseCategories(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []models.DetectorCategory
		wantErr bool
	}{
		{
			name: "claude_sdk",
			in:   "claude_sdk",
			want: []models.DetectorCategory{models.CategoryClaudeSDK},
		},
		{
			// Regression: --detectors openai_sdk is documented in README and
			// ships 12 live rules, but parseCategories used to reject it.
			name: "openai_sdk",
			in:   "openai_sdk",
			want: []models.DetectorCategory{models.CategoryOpenAISDK},
		},
		{
			name: "openshell",
			in:   "openshell",
			want: []models.DetectorCategory{models.CategoryOpenShell},
		},
		{
			// Regression: the combined form from README § Use.
			name: "claude_sdk and openai_sdk combined",
			in:   "claude_sdk,openai_sdk",
			want: []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		},
		{
			name: "whitespace is trimmed",
			in:   " claude_sdk , openai_sdk ",
			want: []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		},
		{
			name:    "unknown category errors",
			in:      "bogus_sdk",
			wantErr: true,
		},
		{
			name:    "one bad entry in a list errors",
			in:      "claude_sdk,bogus_sdk",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCategories(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseCategories(%q): want error, got nil (result %v)", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCategories(%q): unexpected error: %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseCategories(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseCategories(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRulesConfig_FromFlags(t *testing.T) {
	f := scanFlags{rulesRepo: "https://example.com/r", rulesRef: "v1", noRulesUpdate: true}
	rc := rulesConfigFromScan(f)
	if rc.RepoURL != "https://example.com/r" {
		t.Errorf("RepoURL = %q", rc.RepoURL)
	}
	if rc.Ref != "v1" {
		t.Errorf("Ref = %q", rc.Ref)
	}
	if !rc.NoUpdate {
		t.Error("NoUpdate = false, want true")
	}
}

func TestExitCode(t *testing.T) {
	finding := func(sev models.Severity) models.Finding {
		return models.Finding{Severity: sev}
	}

	tests := []struct {
		name     string
		findings []models.Finding
		strict   bool
		want     int
	}{
		{
			name: "no findings exits 0",
			want: 0,
		},
		{
			name:     "only info/low exits 0",
			findings: []models.Finding{finding(models.SeverityInfo), finding(models.SeverityLow)},
			want:     0,
		},
		{
			name:     "a medium finding exits 1",
			findings: []models.Finding{finding(models.SeverityLow), finding(models.SeverityMedium)},
			want:     1,
		},
		{
			name:     "a high finding exits 1",
			findings: []models.Finding{finding(models.SeverityHigh)},
			want:     1,
		},
		{
			name:     "a critical finding exits 1",
			findings: []models.Finding{finding(models.SeverityCritical)},
			want:     1,
		},
		{
			name:     "strict turns a single low into exit 1",
			findings: []models.Finding{finding(models.SeverityLow)},
			strict:   true,
			want:     1,
		},
		{
			name:     "strict with no findings still exits 0",
			findings: nil,
			strict:   true,
			want:     0,
		},
		{
			// --strict floors at low: info/META signals (opaque agent, unused
			// dep, unaudited SDK) must not fail an otherwise-clean CI run.
			name:     "strict with only info findings exits 0",
			findings: []models.Finding{finding(models.SeverityInfo)},
			strict:   true,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exitCode(models.ScanResult{Findings: tt.findings}, tt.strict)
			if got != tt.want {
				t.Fatalf("exitCode(strict=%v, %d findings) = %d, want %d",
					tt.strict, len(tt.findings), got, tt.want)
			}
		})
	}
}

// sampleResult is a minimal ScanResult carrying one finding, enough to exercise
// the report-rendering paths.
func sampleResult() models.ScanResult {
	return models.ScanResult{
		ScanID:   "scan_output_test",
		Manifest: models.ScanManifest{RepoRoot: "."},
		Findings: []models.Finding{{
			RuleID:       "OAI-005",
			Category:     models.CategoryOpenAISDK,
			Severity:     models.SeverityHigh,
			ToolName:     "fetch_url",
			FilePath:     "agents/web.py",
			Line:         42,
			Title:        "Network call has no timeout",
			Explanation:  "An HTTP call without timeout can hang.",
			SuggestedFix: "Pass timeout=5 to the request.",
			Confidence:   0.85,
		}},
	}
}

// TestRenderReport_SARIFMatchesRenderer guards that routing SARIF through the
// CLI's renderReport produces exactly the bytes internal/sarif.Render emits, so
// the byte-stability contract is not perturbed by the --output plumbing.
func TestRenderReport_SARIFMatchesRenderer(t *testing.T) {
	result := sampleResult()
	got, err := renderReport(result, scanFlags{format: "sarif"})
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}
	want := sarif.Render(result, version)
	if !bytes.Equal(got, want) {
		t.Errorf("renderReport(sarif) diverged from sarif.Render")
	}
}

func TestRenderReport_UnknownFormatErrors(t *testing.T) {
	if _, err := renderReport(models.ScanResult{}, scanFlags{format: "xml"}); err == nil {
		t.Fatal("renderReport with unknown format: want error, got nil")
	}
}

// TestWriteReport_ToFile verifies --output writes the report bytes verbatim to
// the given path and writes nothing to stdout.
func TestWriteReport_ToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	payload := []byte("{\"hello\":\"sarif\"}\n")

	if err := writeReport(payload, path); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back report: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file contents = %q, want %q", got, payload)
	}
}

func TestWriteReport_BadPathErrors(t *testing.T) {
	// A path whose parent directory does not exist is a write error the CLI must
	// surface, not swallow.
	missing := filepath.Join(t.TempDir(), "no-such-dir", "out.sarif")
	if err := writeReport([]byte("x"), missing); err == nil {
		t.Fatal("writeReport to a nonexistent directory: want error, got nil")
	}
}

// TestSARIFToFile_RoundTrip exercises the full --output path end to end: render
// SARIF for a result with a medium+ finding, write it to a file, and confirm the
// file holds valid, complete SARIF. This is the exact sequence a code-scanning
// workflow runs before uploading the file, where the scan's nonzero exit code is
// handled by the workflow (if: always()) rather than by losing the report.
func TestSARIFToFile_RoundTrip(t *testing.T) {
	result := sampleResult()
	path := filepath.Join(t.TempDir(), "trustabl.sarif")

	report, err := renderReport(result, scanFlags{format: "sarif"})
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}
	if err := writeReport(report, path); err != nil {
		t.Fatalf("writeReport: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading SARIF file: %v", err)
	}
	if !bytes.Equal(onDisk, sarif.Render(result, version)) {
		t.Error("SARIF file diverged from sarif.Render output")
	}
	// The finding is high severity, so the scan would exit 1; the file write must
	// still have happened (it did, above) so the workflow can upload it.
	if exitCode(result, false) != 1 {
		t.Errorf("exitCode = %d, want 1 (a high finding present)", exitCode(result, false))
	}
}
