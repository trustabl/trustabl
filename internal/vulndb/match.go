// Package vulndb matches a repo's declared dependencies (the BOM from
// analysis.DiscoverDependencies) against a PINNED OSV vulnerability snapshot.
//
// It is the opt-in --vuln-scan layer (TR-271) on top of the repo-wide BOM
// (TR-278). Matching is purely local against a cached snapshot — the scan never
// calls the OSV API, so determinism and the no-network contract hold; the
// snapshot version is folded into ScanID by the caller.
//
// Coverage is honest about its bounds: only dependencies pinned to a CONCRETE
// version are matched. A declared range (^1.0, >=2) cannot be resolved to a
// single version without a lockfile, so it is skipped rather than guessed.
package vulndb

import (
	"regexp"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	pep440 "github.com/aquasecurity/go-pep440-version"

	"github.com/trustabl/trustabl/internal/models"
)

// Record is the subset of the OSV schema (ossf.github.io/osv-schema) needed to
// match a concrete version against a vulnerability's affected set.
type Record struct {
	ID        string     `json:"id"`
	Aliases   []string   `json:"aliases"`
	Summary   string     `json:"summary"`
	Details   string     `json:"details"`
	Severity  []Severity `json:"severity"`
	Affected  []Affected `json:"affected"`
	Withdrawn string     `json:"withdrawn"`
}

type Severity struct {
	Type  string `json:"type"`  // CVSS_V3 / CVSS_V2 / CVSS_V4
	Score string `json:"score"` // CVSS vector string
}

type Affected struct {
	Package  AffectedPackage `json:"package"`
	Ranges   []Range         `json:"ranges"`
	Versions []string        `json:"versions"`
}

type AffectedPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Purl      string `json:"purl"`
}

type Range struct {
	Type   string  `json:"type"` // SEMVER | ECOSYSTEM | GIT
	Events []Event `json:"events"`
}

type Event struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
}

// osvEcosystem maps a trustabl DepRef.Ecosystem to the OSV ecosystem name.
var osvEcosystem = map[string]string{
	"pypi":     "PyPI",
	"npm":      "npm",
	"golang":   "Go",
	"nuget":    "NuGet",
	"composer": "Packagist",
	"cargo":    "crates.io",
}

// AllEcosystems returns every trustabl ecosystem id with OSV coverage, sorted —
// used by `trustabl vulndb pull` to pre-fetch the full database.
func AllEcosystems() []string {
	out := make([]string, 0, len(osvEcosystem))
	for e := range osvEcosystem {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// DB is an OSV snapshot indexed by (OSV-ecosystem, normalized package name).
type DB struct {
	byPkg map[string]map[string][]Record
}

// NewDB builds the lookup index from a flat set of OSV records.
func NewDB(records []Record) *DB {
	db := &DB{byPkg: map[string]map[string][]Record{}}
	for _, rec := range records {
		if rec.Withdrawn != "" {
			continue
		}
		for _, aff := range rec.Affected {
			eco := aff.Package.Ecosystem
			if eco == "" {
				continue
			}
			name := normalizeName(eco, aff.Package.Name)
			if db.byPkg[eco] == nil {
				db.byPkg[eco] = map[string][]Record{}
			}
			db.byPkg[eco][name] = append(db.byPkg[eco][name], rec)
		}
	}
	return db
}

// Len reports the number of distinct vulnerability records indexed.
func (db *DB) Len() int {
	seen := map[string]struct{}{}
	for _, byName := range db.byPkg {
		for _, recs := range byName {
			for _, r := range recs {
				seen[r.ID] = struct{}{}
			}
		}
	}
	return len(seen)
}

// Match returns the vulnerabilities affecting deps, sorted and de-duplicated.
// Only concretely-pinned deps are considered (see package doc).
func Match(deps []models.DepRef, db *DB) []models.DepVuln {
	var out []models.DepVuln
	for _, d := range deps {
		if !isConcreteVersion(d.Version) {
			continue
		}
		osvEco, ok := osvEcosystem[d.Ecosystem]
		if !ok {
			continue
		}
		byName := db.byPkg[osvEco]
		if byName == nil {
			continue
		}
		seen := map[string]struct{}{}
		for _, rec := range byName[normalizeName(osvEco, d.Name)] {
			if _, dup := seen[rec.ID]; dup {
				continue
			}
			if affected, fixedIn := recordAffects(rec, osvEco, d.Ecosystem, d.Version); affected {
				seen[rec.ID] = struct{}{}
				out = append(out, models.DepVuln{
					Dep:      d,
					ID:       rec.ID,
					Aliases:  rec.Aliases,
					Summary:  rec.Summary,
					Severity: severityOf(rec),
					FixedIn:  fixedIn,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Dep.Ecosystem != b.Dep.Ecosystem:
			return a.Dep.Ecosystem < b.Dep.Ecosystem
		case a.Dep.Name != b.Dep.Name:
			return a.Dep.Name < b.Dep.Name
		case a.Dep.Version != b.Dep.Version:
			return a.Dep.Version < b.Dep.Version
		default:
			return a.ID < b.ID
		}
	})
	return out
}

// recordAffects reports whether version (in the given trustabl ecosystem) falls
// in any affected set of rec for the matching OSV ecosystem, and the first known
// fixed version.
func recordAffects(rec Record, osvEco, ecosystem, version string) (bool, string) {
	for _, aff := range rec.Affected {
		if !strings.EqualFold(aff.Package.Ecosystem, osvEco) {
			continue
		}
		for _, v := range aff.Versions {
			if normalizeVersion(ecosystem, v) == normalizeVersion(ecosystem, version) {
				return true, firstFixed(ecosystem, aff)
			}
		}
		for _, r := range aff.Ranges {
			if r.Type == "GIT" {
				continue
			}
			if inRange(ecosystem, version, r) {
				return true, firstFixed(ecosystem, aff)
			}
		}
	}
	return false, ""
}

// inRange implements the OSV range algorithm: scanning events in order, a
// version is affected once an "introduced" <= v is seen and stays affected until
// a "fixed" <= v or "last_affected" < v turns it off. Events are assumed sorted
// ascending (the OSV schema requires this).
func inRange(ecosystem, version string, r Range) bool {
	affected := false
	for _, e := range r.Events {
		switch {
		case e.Introduced == "0":
			affected = true
		case e.Introduced != "":
			if c, ok := compareVersions(ecosystem, version, e.Introduced); ok && c >= 0 {
				affected = true
			}
		case e.Fixed != "":
			if c, ok := compareVersions(ecosystem, version, e.Fixed); ok && c >= 0 {
				affected = false
			}
		case e.LastAffected != "":
			if c, ok := compareVersions(ecosystem, version, e.LastAffected); ok && c > 0 {
				affected = false
			}
		}
	}
	return affected
}

func firstFixed(ecosystem string, aff Affected) string {
	for _, r := range aff.Ranges {
		for _, e := range r.Events {
			if e.Fixed != "" {
				return e.Fixed
			}
		}
	}
	return ""
}

// compareVersions returns -1/0/1 comparing a and b under the ecosystem's version
// semantics (PEP 440 for pypi, semver for the rest), and ok=false if either side
// cannot be parsed.
func compareVersions(ecosystem, a, b string) (int, bool) {
	if ecosystem == "pypi" {
		va, err1 := pep440.Parse(a)
		vb, err2 := pep440.Parse(b)
		if err1 != nil || err2 != nil {
			return 0, false
		}
		return va.Compare(vb), true
	}
	va, err1 := semver.NewVersion(strings.TrimPrefix(a, "v"))
	vb, err2 := semver.NewVersion(strings.TrimPrefix(b, "v"))
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return va.Compare(vb), true
}

// concreteVersionRe matches a single concrete version (digit-led, or Go's
// v-prefixed module form), not a constraint/range.
var concreteVersionRe = regexp.MustCompile(`^v?[0-9][A-Za-z0-9.+_-]*$`)

func isConcreteVersion(v string) bool { return concreteVersionRe.MatchString(v) }

// normalizeName applies ecosystem case/separator rules so a BOM name matches an
// OSV name (PyPI is PEP 503: lowercase, runs of -/_/. collapse to a single -;
// NuGet is case-insensitive; the rest are used as declared).
func normalizeName(osvEco, name string) string {
	switch osvEco {
	case "PyPI":
		return pep503Re.ReplaceAllString(strings.ToLower(name), "-")
	case "NuGet":
		return strings.ToLower(name)
	default:
		return name
	}
}

var pep503Re = regexp.MustCompile(`[-_.]+`)

// normalizeVersion strips Go's "v" prefix for explicit-version-list comparison.
func normalizeVersion(ecosystem, v string) string {
	if ecosystem == "golang" {
		return strings.TrimPrefix(v, "v")
	}
	return v
}

// severityOf buckets an OSV record's CVSS score into a trustabl Severity. With
// no score it defaults to medium (a real vuln with unknown severity is not info).
func severityOf(rec Record) models.Severity {
	best := -1.0
	for _, s := range rec.Severity {
		if score, ok := cvssBaseScore(s.Score); ok && score > best {
			best = score
		}
	}
	switch {
	case best < 0:
		return models.SeverityMedium
	case best >= 9.0:
		return models.SeverityCritical
	case best >= 7.0:
		return models.SeverityHigh
	case best >= 4.0:
		return models.SeverityMedium
	default:
		return models.SeverityLow
	}
}
