// Package cyclonedx renders a Trustabl skill dependency BOM (Story D / TR-221)
// as a CycloneDX 1.5 JSON document.
//
// Output is byte-stable, per the engine's determinism contract: components are
// sorted by a total key, and the document carries no timestamp and no random
// serial number, so identical inputs yield identical bytes. The BOM is pure
// inventory — it states what a skill declares as dependencies and hands off to a
// real SCA tool (OSV-Scanner, Dependabot) for CVE matching. It never asserts a
// vulnerability verdict.
package cyclonedx

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

type doc struct {
	BOMFormat   string      `json:"bomFormat"`
	SpecVersion string      `json:"specVersion"`
	Version     int         `json:"version"`
	Metadata    metadata    `json:"metadata"`
	Components  []component `json:"components"`
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
	Type       string     `json:"type"`
	Name       string     `json:"name"`
	Version    string     `json:"version,omitempty"`
	PURL       string     `json:"purl"`
	Properties []property `json:"properties,omitempty"`
}

type property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// concreteVersionRe matches a declared version that is a single concrete release
// — digit-led, or Go's "v"-prefixed module form (v1.2.3, v0.0.0-<ts>-<sha>); no
// operators or spaces. This is the only form that makes a meaningful purl
// @version. Ranges ("^1.0.0", ">=1 <2") are left off the purl so every emitted
// purl is structurally valid; the declared spec is still carried verbatim in
// component.version.
var concreteVersionRe = regexp.MustCompile(`^v?[0-9][A-Za-z0-9.+_-]*$`)

// Render returns a CycloneDX 1.5 JSON BOM for deps. toolVersion is stamped as
// the generating tool's version. The result is deterministic and byte-stable for
// a given (deps, toolVersion); deps need not be pre-sorted.
func Render(deps []models.DepRef, toolVersion string) []byte {
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

	comps := make([]component, 0, len(sorted))
	for _, d := range sorted {
		c := component{
			Type:    "library",
			Name:    d.Name,
			Version: d.Version,
			PURL:    purl(d),
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
		Components: comps,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(&bom) // encoding a fixed struct never errors
	return buf.Bytes()
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
