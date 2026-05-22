package main

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

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
