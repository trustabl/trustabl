package acac

import "math"

// Readiness gate (spec §5.1). Every threshold and constant lives in this one
// file so the pending corpus-calibration outcome can land as a constants-only
// diff plus a spec amendment. The thresholds are PROVISIONAL: the scoring
// constants they sit on (analysis.Score's saturation/blendK) are explicitly
// uncalibrated, so reliability_score is presented as informational in v0.x.
// The thresholds are versioned with SpecVersion — changing them is a spec
// change, not a silent retune.

// ReadinessLevel is the deployment_readiness value.
type ReadinessLevel string

const (
	ReadinessReady     ReadinessLevel = "ready"
	ReadinessNeedsWork ReadinessLevel = "needs_work"
	ReadinessNotReady  ReadinessLevel = "not_ready"
)

const (
	// ReadyMinScore100 is the minimum score100 for "ready" (additionally
	// requires no high/critical finding on any surface of the selected
	// agent's graph).
	ReadyMinScore100 = 85
	// NotReadyBelowScore100: any score100 strictly below this is
	// "not_ready" regardless of findings.
	NotReadyBelowScore100 = 50
)

// Score100 converts the engine's OverallScore ([0,1] float) to the 0–100
// integer scale with round-half-up semantics, specified explicitly to avoid
// platform drift (math.Round would also round half away from zero, but the
// spec pins half-UP, which differs for negative inputs; scores are
// non-negative so Floor(x+0.5) is exact and unambiguous).
func Score100(overall float64) int {
	return int(math.Floor(overall*100.0 + 0.5))
}

// ReadinessFor applies the spec §5.1 gate to a score and the findings
// attributed to the selected agent's graph (the same set emitted in
// x-trustabl.findings):
//
//	ready     — score100 ≥ ReadyMinScore100 AND no high/critical finding
//	not_ready — score100 < NotReadyBelowScore100 OR any critical finding
//	needs_work — everything else
func ReadinessFor(score100 int, findings []FindingRecord) ReadinessLevel {
	anyCritical := false
	anyHighOrCritical := false
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			anyCritical = true
			anyHighOrCritical = true
		case "high":
			anyHighOrCritical = true
		}
	}
	if score100 < NotReadyBelowScore100 || anyCritical {
		return ReadinessNotReady
	}
	if score100 >= ReadyMinScore100 && !anyHighOrCritical {
		return ReadinessReady
	}
	return ReadinessNeedsWork
}
