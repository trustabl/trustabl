// Package cyclonedx renders a Trustabl repo dependency BOM (TR-278, evolving
// Story D / TR-221) as a CycloneDX 1.5 JSON document.
//
// Output is byte-stable, per the engine's determinism contract: components and
// vulnerabilities are sorted by a total key, and the document carries no
// timestamp and no random serial number, so identical inputs yield identical
// bytes.
//
// By default the BOM is pure inventory — it states what the repo declares as
// dependencies and hands off to a real SCA tool (OSV-Scanner, Dependabot) for
// CVE matching. When the caller ran --vuln-scan, it also passes the matched
// vulnerabilities, which are emitted as a CycloneDX vulnerabilities[] array
// (VEX), each rating-tagged and linked to the component it affects by bom-ref.
// With no vulnerabilities the vulnerabilities[] array is omitted entirely, so an
// inventory-only BOM is unchanged.
package cyclonedx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

type doc struct {
	BOMFormat       string          `json:"bomFormat"`
	SpecVersion     string          `json:"specVersion"`
	Version         int             `json:"version"`
	Metadata        metadata        `json:"metadata"`
	Components      []component     `json:"components"`
	Vulnerabilities []vulnerability `json:"vulnerabilities,omitempty"`
}

type metadata struct {
	Tools []tool `json:"tools"`
}

type tool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type component struct {
	BOMRef     string          `json:"bom-ref"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Version    string          `json:"version,omitempty"`
	PURL       string          `json:"purl"`
	Licenses   []licenseChoice `json:"licenses,omitempty"`
	Properties []property      `json:"properties,omitempty"`
}

type licenseChoice struct {
	License licenseID `json:"license"`
}

type licenseID struct {
	ID string `json:"id"`
}

type property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// vulnerability is a CycloneDX 1.5 vulnerability object (the VEX surface). Each
// is linked to the component it affects via affects[].ref → component.bom-ref.
type vulnerability struct {
	ID             string      `json:"id"`
	Source         *vulnSource `json:"source,omitempty"`
	Ratings        []rating    `json:"ratings,omitempty"`
	Description    string      `json:"description,omitempty"`
	Recommendation string      `json:"recommendation,omitempty"`
	Affects        []affect    `json:"affects,omitempty"`
}

type vulnSource struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type rating struct {
	Severity string `json:"severity"`
	Method   string `json:"method,omitempty"`
}

type affect struct {
	Ref string `json:"ref"`
}

// concreteVersionRe matches a declared version that is a single concrete release
// — digit-led, or Go's "v"-prefixed module form (v1.2.3, v0.0.0-<ts>-<sha>); no
// operators or spaces. This is the only form that makes a meaningful purl
// @version. Ranges ("^1.0.0", ">=1 <2") are left off the purl so every emitted
// purl is structurally valid; the declared spec is still carried verbatim in
// component.version.
var concreteVersionRe = regexp.MustCompile(`^v?[0-9][A-Za-z0-9.+_-]*$`)

// Render returns a CycloneDX 1.5 JSON BOM for deps, optionally carrying vulns
// (the --vuln-scan OSV matches) as a vulnerabilities[] VEX array linked to the
// affected components. toolVersion is stamped as the generating tool's version.
// The result is deterministic and byte-stable for a given (deps, vulns,
// toolVersion); neither slice need be pre-sorted.
func Render(deps []models.DepRef, vulns []models.DepVuln, toolVersion string) []byte {
	sorted := make([]models.DepRef, len(deps))
	copy(sorted, deps)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		switch {
		case a.Ecosystem != b.Ecosystem:
			return a.Ecosystem < b.Ecosystem
		case a.Name != b.Name:
			return a.Name < b.Name
		case a.Version != b.Version:
			return a.Version < b.Version
		default:
			return a.Source < b.Source
		}
	})

	// bom-ref per component. The purl is the natural ref, but dedupeDeps keeps
	// the same package from two manifests (same purl, different source), so a
	// repeated purl is disambiguated with a deterministic "#n" suffix. refByDep
	// lets each vulnerability link back to the exact component it affects.
	comps := make([]component, 0, len(sorted))
	refByDep := make(map[models.DepRef]string, len(sorted))
	seen := map[string]int{}
	for _, d := range sorted {
		base := purl(d)
		ref := base
		if n := seen[base]; n > 0 {
			ref = fmt.Sprintf("%s#%d", base, n)
		}
		seen[base]++
		refByDep[d] = ref

		c := component{
			BOMRef:  ref,
			Type:    "library",
			Name:    d.Name,
			Version: d.Version,
			PURL:    base,
		}
		if d.License != "" {
			c.Licenses = []licenseChoice{{License: licenseID{ID: d.License}}}
		}
		if d.Source != "" {
			c.Properties = []property{{Name: "trustabl:source", Value: d.Source}}
		}
		comps = append(comps, c)
	}

	bom := doc{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: metadata{Tools: []tool{{
			Vendor:  "Trustabl",
			Name:    "trustabl",
			Version: toolVersion,
		}}},
		Components:      comps,
		Vulnerabilities: renderVulns(vulns, refByDep),
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&bom) // encoding a fixed struct never errors
	return buf.Bytes()
}

// renderVulns maps the OSV matches to CycloneDX vulnerability objects, sorted for
// byte-stability and linked to their component by bom-ref. Returns nil (omitted
// array) when there are no vulnerabilities, keeping the inventory-only BOM
// unchanged.
func renderVulns(vulns []models.DepVuln, refByDep map[models.DepRef]string) []vulnerability {
	if len(vulns) == 0 {
		return nil
	}
	sorted := make([]models.DepVuln, len(vulns))
	copy(sorted, vulns)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		switch {
		case vulnID(a) != vulnID(b):
			return vulnID(a) < vulnID(b)
		case a.Dep.Ecosystem != b.Dep.Ecosystem:
			return a.Dep.Ecosystem < b.Dep.Ecosystem
		case a.Dep.Name != b.Dep.Name:
			return a.Dep.Name < b.Dep.Name
		case a.Dep.Version != b.Dep.Version:
			return a.Dep.Version < b.Dep.Version
		default:
			return a.Dep.Source < b.Dep.Source
		}
	})

	out := make([]vulnerability, 0, len(sorted))
	for _, v := range sorted {
		rec := "No fixed version is published; review the advisory and consider an alternative or mitigation."
		if v.FixedIn != "" {
			rec = fmt.Sprintf("Upgrade %s to %s or later.", v.Dep.Name, v.FixedIn)
		}
		vu := vulnerability{
			ID:             vulnID(v),
			Source:         &vulnSource{Name: "OSV", URL: "https://osv.dev/vulnerability/" + v.ID},
			Ratings:        []rating{{Severity: string(v.Severity), Method: "other"}},
			Description:    v.Summary,
			Recommendation: rec,
		}
		if ref := refByDep[v.Dep]; ref != "" {
			vu.Affects = []affect{{Ref: ref}}
		}
		out = append(out, vu)
	}
	return out
}

// vulnID prefers the CVE alias (matching the synthesized finding's RuleID, so the
// report and the BOM cross-reference cleanly) and falls back to the OSV id.
func vulnID(v models.DepVuln) string {
	if len(v.Aliases) > 0 {
		return v.Aliases[0]
	}
	return v.ID
}

// purl builds a Package URL for a dependency. Scoped npm names (@scope/pkg) get
// their leading "@" percent-encoded per the purl spec; a concrete version is
// appended as @version, while a range is omitted (carried in component.version
// instead) so the identifier stays valid.
func purl(d models.DepRef) string {
	name := d.Name
	if len(name) > 0 && name[0] == '@' {
		name = "%40" + name[1:]
	}
	p := "pkg:" + d.Ecosystem + "/" + name
	if concreteVersionRe.MatchString(d.Version) {
		p += "@" + d.Version
	}
	return p
}
