package acac

import "math"

// Readiness gate (spec §5.1). Every threshold and constant lives in this one
// file so a future calibration outcome can land as a constants-only diff plus
// a spec amendment. The 2026-06-10 calibration run (17 human-labeled anchors,
// .superpowers/calibration/) kept the shipped thresholds: the best grid
// alternative won by a single anchor while introducing false-not_readies, and
// the real misses were structural, not threshold-shaped. reliability_score
// stays presented as informational pending a larger-corpus re-run. The
// thresholds are versioned with SpecVersion — changing them is a spec change,
// not a silent retune.

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

// ReadinessFor applies the spec §5.1 gate to a score, the findings attributed
// to the selected agent's graph (the same set emitted in x-trustabl.findings),
// and the audit-coverage facts:
//
//	ready     — score100 ≥ ReadyMinScore100 AND no high/critical finding
//	            AND at least one audited surface AND no unaudited SDK observed
//	not_ready — score100 < NotReadyBelowScore100 OR any critical finding
//	needs_work — everything else
//
// The coverage conditions are the unauditable-repo guard (§5.1 amendment,
// 2026-06-10): a scan that audited nothing scores a vacuous 1.0, and a repo
// using an SDK Trustabl does not audit can hide arbitrary risk behind a clean
// bill. Absence of evidence caps the verdict at needs_work — it never makes a
// repo "ready", and it is not treated as evidence of badness either (no
// not_ready downgrade).
func ReadinessFor(score100 int, findings []FindingRecord, graphSurfaces, unauditedSDKs int) ReadinessLevel {
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
	if score100 >= ReadyMinScore100 && !anyHighOrCritical && graphSurfaces > 0 && unauditedSDKs == 0 {
		return ReadinessReady
	}
	return ReadinessNeedsWork
}
