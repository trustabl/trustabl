package scanner

import (
	"fmt"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/vulndb"
)

// runVulnScan is the opt-in --vuln-scan layer (TR-271): it resolves a PINNED OSV
// snapshot for the ecosystems present in the BOM and matches the declared deps
// against it locally. The snapshot version is returned so the caller can fold it
// into ScanID (keeping the ID honest about which vuln data produced the result).
// The scan never queries the OSV API per dependency — matching is against the
// cached snapshot only.
//
// rep drives the live progress display: a step bar over the ecosystems being
// pulled, with a status line showing the current download (and its byte count)
// or the cache hit. Progress is stderr-only and never affects the result.
func runVulnScan(deps []models.DepRef, noUpdate bool, cacheDir string, rep progress.Reporter) (vulns []models.DepVuln, version string, err error) {
	sized := false
	res, err := vulndb.Resolve(vulndb.ResolveConfig{
		Ecosystems: depEcosystems(deps),
		NoUpdate:   noUpdate,
		CacheDir:   cacheDir,
		OnProgress: func(p vulndb.ResolveProgress) {
			if !sized {
				rep.SetTotal(p.Total) // size the bar to the ecosystem count on the first event
				sized = true
			}
			switch {
			case p.Finished:
				rep.Advance(finishedVulnDetail(p))
			case p.BytesRead > 0:
				rep.SetDetail(fmt.Sprintf("downloading %s database — %s", p.OSVEcosystem, humanizeBytes(p.BytesRead)))
			default:
				rep.SetDetail("resolving " + p.OSVEcosystem + " database…")
			}
		},
	})
	if err != nil {
		return nil, "", err
	}
	rep.SetDetail(fmt.Sprintf("matching %d dependencies against the OSV snapshot", len(deps)))
	return vulndb.Match(deps, res.DB), res.Version, nil
}

// finishedVulnDetail renders the persistent detail for a resolved ecosystem,
// e.g. "PyPI — 4821 advisories (cached)".
func finishedVulnDetail(p vulndb.ResolveProgress) string {
	src := "fetched"
	if p.FromCache {
		src = "cached"
	}
	return fmt.Sprintf("%s — %d advisories (%s)", p.OSVEcosystem, p.Records, src)
}

// humanizeBytes formats a byte count for the download status line.
func humanizeBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
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
