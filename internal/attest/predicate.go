// Package attest builds Trustabl scan attestations and drives the cosign CLI to
// sign and verify them. Trustabl holds no signing keys of its own: it produces a
// deterministic in-toto predicate from a ScanResult and shells out to cosign,
// which owns all of the keyless (Fulcio/Rekor/OIDC) or private-key cryptography.
//
// The unit of trust is the canonical scan-report JSON: its sha256 is the
// attestation subject, so a verifier binds the predicate to the exact bytes
// Trustabl produced. A repo has no single natural artifact digest, and the report
// already pins the scanned state (its ScanID folds the code + rules identity), so
// signing the report is both simpler and more reproducible than signing a source
// archive.
package attest

import (
	"bytes"
	"encoding/json"

	"github.com/trustabl/trustabl/internal/models"
)

// PredicateType is the in-toto predicateType URI stamped on every Trustabl scan
// attestation. The /v1 suffix versions the predicate schema: a consumer pins this
// exact URI, so a future breaking change ships as /v2 rather than silently
// redefining v1.
const PredicateType = "https://trustabl.dev/attestation/scan/v1"

// Verdict values. "fail" means the scan found at least one finding of medium
// severity or higher — the same gate the default (non-strict) exit code uses
// (see cmd/trustabl exitCode), so a consumer reading the predicate verdict and a
// CI step reading the exit code agree. info/low findings do not fail the verdict.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// SeverityCounts is the per-severity finding histogram embedded in the predicate.
// A fixed-field struct (not a map) keeps the rendered JSON byte-stable.
type SeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// Predicate is the claim a Trustabl scan attestation makes about the scanned repo.
// Every field is derived deterministically from the ScanResult (plus the engine
// version, threaded in like the SARIF renderer takes it) — no wall-clock, no map
// iteration — so the same scan yields byte-identical predicate bytes. It is a
// downstream artifact and is deliberately NOT folded into ScanID.
//
// There is intentionally no commit field in v1: the data model carries no commit
// SHA, and the subject (the report's sha256) plus ScanID already pin the scanned
// state cryptographically. Binding a human-friendly commit label is a follow-up.
type Predicate struct {
	ScanID         string         `json:"scanId"`
	EngineVersion  string         `json:"engineVersion"`
	RulesSHA       string         `json:"rulesSha"`
	RulesOrigin    string         `json:"rulesOrigin"`
	Repo           string         `json:"repo"`
	OverallScore   float64        `json:"overallScore"`
	Verdict        string         `json:"verdict"`
	SeverityCounts SeverityCounts `json:"severityCounts"`
}

// BuildPredicate derives the deterministic predicate from a scan result.
// engineVersion is threaded in from the CLI (the same value the SARIF renderer
// receives) because the build version lives in package main, not here.
func BuildPredicate(r models.ScanResult, engineVersion string) Predicate {
	var counts SeverityCounts
	for _, f := range r.Findings {
		switch f.Severity {
		case models.SeverityCritical:
			counts.Critical++
		case models.SeverityHigh:
			counts.High++
		case models.SeverityMedium:
			counts.Medium++
		case models.SeverityLow:
			counts.Low++
		case models.SeverityInfo:
			counts.Info++
		}
	}
	verdict := VerdictPass
	if counts.Critical > 0 || counts.High > 0 || counts.Medium > 0 {
		verdict = VerdictFail
	}
	return Predicate{
		ScanID:         r.ScanID,
		EngineVersion:  engineVersion,
		RulesSHA:       r.RulesVersion,
		RulesOrigin:    r.RulesOrigin.Tag(),
		Repo:           r.Repo,
		OverallScore:   r.OverallScore,
		Verdict:        verdict,
		SeverityCounts: counts,
	}
}

// JSON renders the predicate as canonical, indented bytes with a trailing newline
// — the same shape as the scan report (cmd/trustabl jsonBytes), so the file
// Trustabl writes and the bytes cosign embeds are stable and diffable.
func (p Predicate) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
