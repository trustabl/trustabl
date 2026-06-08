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
// rep drives two live phases, owned here so the work and the UI stay together:
//
//  1. "Resolving vulnerability database" — the OSV pull. A determinate bar is
//     shown ONLY while actually downloading, where it climbs by bytes
//     (Content-Length) so a big pull fills 0→100%. A cache load is instant and
//     has no bytes, so it shows a spinner + status line instead of a meaningless
//     step bar that would just jump 0→100% per database.
//  2. "Scanning dependencies" — the local match, with a fresh bar that advances
//     per package and a status line naming the package currently being scanned.
//
// Progress is stderr-only and never affects the result.
func runVulnScan(deps []models.DepRef, noUpdate bool, cacheDir string, rep progress.Reporter) (vulns []models.DepVuln, version string, err error) {
	rep.StartPhase("vuln-resolve", "Resolving vulnerability database")
	res, err := vulndb.Resolve(vulndb.ResolveConfig{
		Ecosystems: depEcosystems(deps),
		NoUpdate:   noUpdate,
		CacheDir:   cacheDir,
		OnProgress: func(p vulndb.ResolveProgress) {
			total := p.Total
			if total < 1 {
				total = 1
			}
			idx := float64(p.Index - 1) // 0-based slot for this ecosystem
			switch {
			case p.BytesRead > 0 && p.BytesTotal > 0:
				// Real download with a known size → determinate byte bar.
				frac := float64(p.BytesRead) / float64(p.BytesTotal)
				if frac > 1 {
					frac = 1
				}
				rep.SetProgress((idx+frac)/float64(total),
					fmt.Sprintf("downloading %s (%d/%d) — %s / %s", p.OSVEcosystem, p.Index, total, humanizeBytes(p.BytesRead), humanizeBytes(p.BytesTotal)))
			case p.BytesRead > 0:
				// Downloading, size unknown → spinner + running byte count.
				rep.SetDetail(fmt.Sprintf("downloading %s (%d/%d) — %s", p.OSVEcosystem, p.Index, total, humanizeBytes(p.BytesRead)))
			case p.Finished && p.FromCache:
				// Cache load is instant → spinner + status, no jumpy step bar.
				rep.SetDetail(fmt.Sprintf("loaded %s — %d advisories (cached, %d/%d)", p.OSVEcosystem, p.Records, p.Index, total))
			case p.Finished:
				// A downloaded ecosystem finished → leave its byte bar full.
				rep.SetProgress(float64(p.Index)/float64(total), finishedVulnDetail(p))
			default:
				// Ecosystem started; not yet known whether it downloads or is
				// cached → spinner only, no bar.
				rep.SetDetail("resolving " + p.OSVEcosystem + " database…")
			}
		},
	})
	if err != nil {
		rep.EndPhase("unavailable")
		return nil, "", err
	}
	rep.EndPhase(fmt.Sprintf("%d advisories", res.DB.Len()))

	// Phase 2: scan each declared dependency against the snapshot. The bar
	// advances per package and the detail names the one being scanned; updates are
	// throttled to ~100 frames so a huge BOM doesn't flood the render channel.
	rep.StartPhase("vuln-scan", "Scanning dependencies")
	total := len(deps)
	step := total / 100
	if step < 1 {
		step = 1
	}
	scanned := 0
	vulns = vulndb.Match(deps, res.DB, func(d models.DepRef) {
		scanned++
		if scanned == total || scanned%step == 0 {
			rep.SetProgress(float64(scanned)/float64(total),
				fmt.Sprintf("scanning %s %s (%d/%d)", d.Name, d.Version, scanned, total))
		}
	})
	rep.EndPhase(fmt.Sprintf("%d vulnerabilities across %d dependencies", len(vulns), total))
	return vulns, res.Version, nil
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
			StartLine:    v.Dep.StartLine,
			EndLine:      v.Dep.EndLine,
			Title:        fmt.Sprintf("Vulnerable dependency: %s %s (%s)", v.Dep.Name, v.Dep.Version, id),
			Explanation:  expl,
			SuggestedFix: fix,
			Confidence:   1.0,
		})
	}
	return out
}
