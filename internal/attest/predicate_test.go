package attest

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func sampleResult() models.ScanResult {
	return models.ScanResult{
		ScanID:       "scan-123",
		Repo:         "https://github.com/owner/repo",
		OverallScore: 0.5,
		RulesVersion: "abc123",
		RulesOrigin:  models.RulesOrigin{Signed: true, Channel: "production"},
		Findings: []models.Finding{
			{Severity: models.SeverityHigh},
			{Severity: models.SeverityMedium},
			{Severity: models.SeverityLow},
			{Severity: models.SeverityInfo},
		},
	}
}

// TestBuildPredicate_JSONGolden pins the exact predicate bytes: the attestation
// is only as reproducible as this rendering, so a formatting change must be a
// deliberate, reviewed diff (and, per the /vN convention, a new predicate type).
func TestBuildPredicate_JSONGolden(t *testing.T) {
	got, err := BuildPredicate(sampleResult(), "1.2.3").JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	want := `{
  "scanId": "scan-123",
  "engineVersion": "1.2.3",
  "rulesSha": "abc123",
  "rulesOrigin": "signed:production",
  "repo": "https://github.com/owner/repo",
  "overallScore": 0.5,
  "verdict": "fail",
  "severityCounts": {
    "critical": 0,
    "high": 1,
    "medium": 1,
    "low": 1,
    "info": 1
  }
}
`
	if string(got) != want {
		t.Errorf("predicate JSON mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildPredicate_Deterministic guards the determinism contract: the same scan
// must render byte-identical predicate bytes every time (no map iteration, no
// wall-clock leaking in).
func TestBuildPredicate_Deterministic(t *testing.T) {
	r := sampleResult()
	a, err := BuildPredicate(r, "1.2.3").JSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildPredicate(r, "1.2.3").JSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("non-deterministic predicate bytes:\n%s\n!=\n%s", a, b)
	}
}

// TestBuildPredicate_Verdict checks the pass/fail gate mirrors the default
// (non-strict) exit code: medium-or-higher fails, info/low pass.
func TestBuildPredicate_Verdict(t *testing.T) {
	cases := []struct {
		name     string
		findings []models.Finding
		want     string
	}{
		{"empty", nil, VerdictPass},
		{"info only", []models.Finding{{Severity: models.SeverityInfo}}, VerdictPass},
		{"low only", []models.Finding{{Severity: models.SeverityLow}}, VerdictPass},
		{"medium fails", []models.Finding{{Severity: models.SeverityMedium}}, VerdictFail},
		{"high fails", []models.Finding{{Severity: models.SeverityHigh}}, VerdictFail},
		{"critical fails", []models.Finding{{Severity: models.SeverityCritical}}, VerdictFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := BuildPredicate(models.ScanResult{Findings: tc.findings}, "v")
			if p.Verdict != tc.want {
				t.Errorf("verdict = %q, want %q", p.Verdict, tc.want)
			}
		})
	}
}
