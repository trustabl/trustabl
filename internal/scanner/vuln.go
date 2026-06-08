package scanner

import (
	"fmt"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/vulndb"
)

// runVulnScan is the opt-in --vuln-scan layer (TR-271): it resolves a PINNED OSV
// snapshot for the ecosystems present in the BOM and matches the declared deps
// against it locally. The snapshot version is returned so the caller can fold it
// into ScanID (keeping the ID honest about which vuln data produced the result).
// The scan never queries the OSV API per dependency — matching is against the
// cached snapshot only.
func runVulnScan(deps []models.DepRef, noUpdate bool, cacheDir string) (vulns []models.DepVuln, version string, err error) {
	res, err := vulndb.Resolve(vulndb.ResolveConfig{
		Ecosystems: depEcosystems(deps),
		NoUpdate:   noUpdate,
		CacheDir:   cacheDir,
	})
	if err != nil {
		return nil, "", err
	}
	return vulndb.Match(deps, res.DB), res.Version, nil
}

// depEcosystems returns the distinct ecosystems present in the BOM, so the vulndb
// resolver fetches only the OSV databases that can match this repo.
func depEcosystems(deps []models.DepRef) []string {
	set := map[string]struct{}{}
	for _, d := range deps {
		if d.Ecosystem != "" {
			set[d.Ecosystem] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// vulnFindings synthesizes one Finding per matched vulnerability so vulns flow
// through the normal findings pipeline — exit codes, SARIF, and the report. This
// is the dependency analog of the META-finding path, not a YAML rule.
func vulnFindings(vulns []models.DepVuln) []models.Finding {
	out := make([]models.Finding, 0, len(vulns))
	for _, v := range vulns {
		id := v.ID
		if len(v.Aliases) > 0 {
			id = v.Aliases[0] // prefer the CVE id when present
		}
		fix := "No fixed version is published; review the advisory and consider an alternative or mitigation."
		if v.FixedIn != "" {
			fix = fmt.Sprintf("Upgrade %s to %s or later.", v.Dep.Name, v.FixedIn)
		}
		expl := v.Summary
		if expl == "" {
			expl = fmt.Sprintf("%s %s has a known vulnerability (%s).", v.Dep.Name, v.Dep.Version, v.ID)
		}
		out = append(out, models.Finding{
			RuleID:       id,
			Severity:     v.Severity,
			ToolName:     v.Dep.Name,
			FilePath:     v.Dep.Source,
			Title:        fmt.Sprintf("Vulnerable dependency: %s %s (%s)", v.Dep.Name, v.Dep.Version, id),
			Explanation:  expl,
			SuggestedFix: fix,
			Confidence:   1.0,
		})
	}
	return out
}
