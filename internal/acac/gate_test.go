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
		name     string
		score    int
		findings []FindingRecord
		want     ReadinessLevel
	}{
		{"85 clean is ready", 85, nil, ReadinessReady},
		{"84 clean is needs_work", 84, nil, ReadinessNeedsWork},
		{"50 clean is needs_work", 50, nil, ReadinessNeedsWork},
		{"49 clean is not_ready", 49, nil, ReadinessNotReady},
		{"100 with high is needs_work", 100, high, ReadinessNeedsWork},
		{"100 with critical is not_ready (critical override)", 100, critical, ReadinessNotReady},
		{"85 with medium only is ready", 85, medium, ReadinessReady},
		{"49 with critical is not_ready", 49, critical, ReadinessNotReady},
	}
	for _, c := range cases {
		if got := ReadinessFor(c.score, c.findings); got != c.want {
			t.Errorf("%s: ReadinessFor(%d) = %s, want %s", c.name, c.score, got, c.want)
		}
	}
}
