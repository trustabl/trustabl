package scanner

import (
	"fmt"

	"github.com/trustabl/trustabl/internal/models"
)

// copyleftEntry describes a copyleft license family for finding synthesis.
type copyleftEntry struct {
	family   string
	severity models.Severity
	ruleID   string
}

// copyleftLicenses maps SPDX identifiers to their finding metadata.
// Severity is medium: license incompatibility is a legal/compliance risk,
// not an active exploit — the same rationale as vuln findings at medium.
var copyleftLicenses = map[string]copyleftEntry{
	"GPL-2.0-only":     {"GPL-2.0", models.SeverityMedium, "LIC-GPL2"},
	"GPL-2.0-or-later": {"GPL-2.0", models.SeverityMedium, "LIC-GPL2"},
	"GPL-3.0-only":     {"GPL-3.0", models.SeverityMedium, "LIC-GPL3"},
	"GPL-3.0-or-later": {"GPL-3.0", models.SeverityMedium, "LIC-GPL3"},
	"AGPL-3.0-only":    {"AGPL-3.0", models.SeverityMedium, "LIC-AGPL3"},
	"AGPL-3.0-or-later": {"AGPL-3.0", models.SeverityMedium, "LIC-AGPL3"},
	"LGPL-2.1-only":    {"LGPL-2.1", models.SeverityMedium, "LIC-LGPL21"},
	"LGPL-2.1-or-later": {"LGPL-2.1", models.SeverityMedium, "LIC-LGPL21"},
	"SSPL-1.0":         {"SSPL-1.0", models.SeverityMedium, "LIC-SSPL1"},
}

// licenseFindings synthesizes one Finding per dependency that carries a
// copyleft license, so license violations flow through the normal findings
// pipeline — exit codes, SARIF, and the report. This is the --license-scan
// analog of vulnFindings, not a YAML rule.
func licenseFindings(deps []models.DepRef) []models.Finding {
	var out []models.Finding
	for _, d := range deps {
		if d.License == "" {
			continue
		}
		entry, ok := copyleftLicenses[d.License]
		if !ok {
			continue
		}
		out = append(out, models.Finding{
			RuleID:    entry.ruleID,
			Severity:  entry.severity,
			FilePath:  d.Source,
			StartLine: d.StartLine,
			EndLine:   d.EndLine,
			Title:     fmt.Sprintf("Copyleft dependency: %s (%s)", d.Name, d.License),
			Explanation: fmt.Sprintf(
				"%s declares the %s license (%s). Copyleft licenses impose "+
					"redistribution obligations: if this dependency is linked into "+
					"an agent or service distributed to third parties, the "+
					"combined work may need to be released under the same license. "+
					"Review your distribution model and confirm this is intentional.",
				d.Name, entry.family, d.License),
			SuggestedFix: fmt.Sprintf(
				"Verify that your project's license is compatible with %s. "+
					"If redistribution obligations are unacceptable, replace %s "+
					"with a permissively licensed alternative (MIT, Apache-2.0, BSD).",
				entry.family, d.Name),
			Confidence: 0.9,
		})
	}
	return out
}