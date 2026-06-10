package acac

import "testing"

func TestScore100RoundHalfUp(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0.0, 0},
		{1.0, 100},
		{0.61, 61},
		{0.845, 85}, // half rounds up
		{0.85, 85},
		{0.844, 84},
		{0.495, 50}, // half rounds up at the not_ready boundary
		{0.494, 49},
	}
	for _, c := range cases {
		if got := Score100(c.in); got != c.want {
			t.Errorf("Score100(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestReadinessThresholdBoundaries(t *testing.T) {
	high := []FindingRecord{{ID: "X-1", Severity: "high"}}
	critical := []FindingRecord{{ID: "X-2", Severity: "critical"}}
	medium := []FindingRecord{{ID: "X-3", Severity: "medium"}}

	cases := []struct {
		name      string
		score     int
		findings  []FindingRecord
		surfaces  int
		unaudited int
		want      ReadinessLevel
	}{
		{"85 clean is ready", 85, nil, 1, 0, ReadinessReady},
		{"84 clean is needs_work", 84, nil, 1, 0, ReadinessNeedsWork},
		{"50 clean is needs_work", 50, nil, 1, 0, ReadinessNeedsWork},
		{"49 clean is not_ready", 49, nil, 1, 0, ReadinessNotReady},
		{"100 with high is needs_work", 100, high, 1, 0, ReadinessNeedsWork},
		{"100 with critical is not_ready (critical override)", 100, critical, 1, 0, ReadinessNotReady},
		{"85 with medium only is ready", 85, medium, 1, 0, ReadinessReady},
		{"49 with critical is not_ready", 49, critical, 1, 0, ReadinessNotReady},
		// Unauditable-repo guard (§5.1 amendment): nothing audited or an
		// unaudited SDK observed caps the verdict at needs_work — a vacuous
		// 1.0 must never gate ready, and absence of evidence is not evidence
		// of badness (no not_ready downgrade).
		{"100 clean but zero surfaces is needs_work", 100, nil, 0, 0, ReadinessNeedsWork},
		{"100 clean but unaudited SDK is needs_work", 100, nil, 3, 1, ReadinessNeedsWork},
		{"49 with zero surfaces is still not_ready", 49, nil, 0, 0, ReadinessNotReady},
		{"100 critical with unaudited is still not_ready", 100, critical, 1, 2, ReadinessNotReady},
	}
	for _, c := range cases {
		if got := ReadinessFor(c.score, c.findings, c.surfaces, c.unaudited); got != c.want {
			t.Errorf("%s: ReadinessFor(%d) = %s, want %s", c.name, c.score, got, c.want)
		}
	}
}
