package cyclonedx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func sampleDeps() []models.DepRef {
	return []models.DepRef{
		{Name: "requests", Version: "2.31.0", Ecosystem: "pypi", Source: "skills/a/requirements.txt"},
		{Name: "flask", Version: ">=1.0,<2.0", Ecosystem: "pypi", Source: "skills/a/requirements.txt"},
		{Name: "@types/node", Version: "20.1.0", Ecosystem: "npm", Source: "skills/b/package.json"},
	}
}

// TestRender_OrderIndependent proves the BOM is byte-stable regardless of input
// order — the determinism contract.
func TestRender_OrderIndependent(t *testing.T) {
	deps := sampleDeps()
	a := Render(deps, nil, "1.2.3")
	shuffled := []models.DepRef{deps[2], deps[0], deps[1]}
	b := Render(shuffled, nil, "1.2.3")
	if !bytes.Equal(a, b) {
		t.Fatalf("Render not order-independent:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// TestRender_NoNondeterministicFields guards against a timestamp or random
// serial number leaking into the byte-stable output.
func TestRender_NoNondeterministicFields(t *testing.T) {
	out := string(Render(sampleDeps(), nil, "1.2.3"))
	for _, banned := range []string{"timestamp", "serialNumber"} {
		if strings.Contains(out, banned) {
			t.Errorf("BOM contains nondeterministic field %q:\n%s", banned, out)
		}
	}
}

func TestRender_Structure(t *testing.T) {
	out := Render(sampleDeps(), nil, "9.9.9")

	var doc struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
		Metadata    struct {
			Tools []struct {
				Vendor  string `json:"vendor"`
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"tools"`
		} `json:"metadata"`
		Components []struct {
			Type       string `json:"type"`
			Name       string `json:"name"`
			Version    string `json:"version"`
			PURL       string `json:"purl"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid CycloneDX JSON: %v\n%s", err, out)
	}
	if doc.BOMFormat != "CycloneDX" || doc.SpecVersion != "1.5" || doc.Version != 1 {
		t.Errorf("bad header: format=%q spec=%q version=%d", doc.BOMFormat, doc.SpecVersion, doc.Version)
	}
	if len(doc.Metadata.Tools) != 1 || doc.Metadata.Tools[0].Name != "trustabl" || doc.Metadata.Tools[0].Version != "9.9.9" {
		t.Errorf("bad tool metadata: %+v", doc.Metadata.Tools)
	}
	if len(doc.Components) != 3 {
		t.Fatalf("want 3 components, got %d: %+v", len(doc.Components), doc.Components)
	}

	purls := map[string]string{} // purl -> declared version
	for _, c := range doc.Components {
		if c.Type != "library" {
			t.Errorf("component %q type = %q, want library", c.Name, c.Type)
		}
		if len(c.Properties) != 1 || c.Properties[0].Name != "trustabl:source" {
			t.Errorf("component %q missing trustabl:source property: %+v", c.Name, c.Properties)
		}
		purls[c.PURL] = c.Version
	}
	// A concrete version yields an @version purl.
	if _, ok := purls["pkg:pypi/requests@2.31.0"]; !ok {
		t.Errorf("concrete-version purl missing; got %v", purls)
	}
	// A range yields a versionless purl (spec stays in component.version).
	if v, ok := purls["pkg:pypi/flask"]; !ok || v != ">=1.0,<2.0" {
		t.Errorf("range purl/version wrong; got %v", purls)
	}
	// A scoped npm name percent-encodes the leading '@'.
	if _, ok := purls["pkg:npm/%40types/node@20.1.0"]; !ok {
		t.Errorf("scoped-npm purl wrong; got %v", purls)
	}
}

func TestRender_EmptyIsEmptyArray(t *testing.T) {
	out := string(Render(nil, nil, "1.0.0"))
	if !strings.Contains(out, `"components": []`) {
		t.Errorf("empty BOM should render an empty components array, got:\n%s", out)
	}
}

// TestRender_AllEcosystemPurls locks the purl mapping for the non-pypi/npm
// ecosystems — including Go's v-prefixed module versions (which must stay in the
// purl) and a composer slash-name with a range version (versionless purl).
func TestRender_AllEcosystemPurls(t *testing.T) {
	deps := []models.DepRef{
		{Name: "github.com/foo/bar", Version: "v1.2.3", Ecosystem: "golang", Source: "go.mod"},
		{Name: "Newtonsoft.Json", Version: "13.0.1", Ecosystem: "nuget", Source: "x.csproj"},
		{Name: "serde", Version: "1.0.190", Ecosystem: "cargo", Source: "Cargo.toml"},
		{Name: "monolog/monolog", Version: "^2.0", Ecosystem: "composer", Source: "composer.json"},
	}
	out := string(Render(deps, nil, "1.0.0"))
	for _, want := range []string{
		`"purl": "pkg:golang/github.com/foo/bar@v1.2.3"`, // Go v-prefix kept
		`"purl": "pkg:nuget/Newtonsoft.Json@13.0.1"`,
		`"purl": "pkg:cargo/serde@1.0.190"`,
		`"purl": "pkg:composer/monolog/monolog"`, // range → versionless purl
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

// TestRender_Vulnerabilities proves --vuln-scan matches are emitted as a
// CycloneDX VEX vulnerabilities[] array: CVE-preferred id, OSV source, severity
// rating, fix recommendation, and an affects[] ref that links to the affected
// component's bom-ref.
func TestRender_Vulnerabilities(t *testing.T) {
	deps := []models.DepRef{
		{Name: "requests", Version: "2.19.0", Ecosystem: "pypi", Source: "requirements.txt"},
	}
	vulns := []models.DepVuln{{
		Dep:      deps[0],
		ID:       "GHSA-x84v-xcm2-53pg",
		Aliases:  []string{"CVE-2018-18074"},
		Summary:  "Requests leaks Authorization on redirect",
		Severity: models.SeverityHigh,
		FixedIn:  "2.20.0",
	}}
	out := Render(deps, vulns, "1.0.0")

	var doc struct {
		Components []struct {
			BOMRef string `json:"bom-ref"`
			PURL   string `json:"purl"`
		} `json:"components"`
		Vulnerabilities []struct {
			ID     string `json:"id"`
			Source struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"source"`
			Ratings []struct {
				Severity string `json:"severity"`
				Method   string `json:"method"`
			} `json:"ratings"`
			Recommendation string `json:"recommendation"`
			Affects        []struct {
				Ref string `json:"ref"`
			} `json:"affects"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid CycloneDX JSON: %v\n%s", err, out)
	}
	if len(doc.Components) != 1 || doc.Components[0].BOMRef == "" {
		t.Fatalf("component/bom-ref missing: %+v", doc.Components)
	}
	if len(doc.Vulnerabilities) != 1 {
		t.Fatalf("want 1 vulnerability, got %d:\n%s", len(doc.Vulnerabilities), out)
	}
	v := doc.Vulnerabilities[0]
	if v.ID != "CVE-2018-18074" {
		t.Errorf("id = %q, want CVE alias preferred", v.ID)
	}
	if v.Source.Name != "OSV" || !strings.Contains(v.Source.URL, "GHSA-x84v-xcm2-53pg") {
		t.Errorf("source = %+v, want OSV + osv.dev URL keyed by OSV id", v.Source)
	}
	if len(v.Ratings) != 1 || v.Ratings[0].Severity != "high" {
		t.Errorf("ratings = %+v, want one 'high' rating", v.Ratings)
	}
	if !strings.Contains(v.Recommendation, "2.20.0") {
		t.Errorf("recommendation = %q, want the fixed version", v.Recommendation)
	}
	if len(v.Affects) != 1 || v.Affects[0].Ref != doc.Components[0].BOMRef {
		t.Errorf("affects %+v must link the component bom-ref %q", v.Affects, doc.Components[0].BOMRef)
	}
}

// TestRender_VulnsOrderIndependent extends the determinism contract to the
// vulnerabilities[] array.
func TestRender_VulnsOrderIndependent(t *testing.T) {
	deps := sampleDeps()
	vulns := []models.DepVuln{
		{Dep: deps[0], ID: "GHSA-2", Severity: models.SeverityLow},
		{Dep: deps[0], ID: "GHSA-1", Aliases: []string{"CVE-1"}, Severity: models.SeverityHigh, FixedIn: "9.0.0"},
	}
	a := Render(deps, vulns, "1")
	b := Render(deps, []models.DepVuln{vulns[1], vulns[0]}, "1")
	if !bytes.Equal(a, b) {
		t.Fatalf("vulnerabilities render not order-independent:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// TestRender_NoVulnsOmitsArray proves an inventory-only BOM (no --vuln-scan)
// omits the vulnerabilities array entirely, so it stays unchanged.
func TestRender_NoVulnsOmitsArray(t *testing.T) {
	out := string(Render(sampleDeps(), nil, "1.0.0"))
	if strings.Contains(out, "vulnerabilities") {
		t.Errorf("inventory-only BOM must not contain a vulnerabilities array:\n%s", out)
	}
}

// TestRender_License proves a dep with a License field emits a CycloneDX
// licenses[] array and a dep without one omits it.
func TestRender_License(t *testing.T) {
	deps := []models.DepRef{
		{Name: "requests", Version: "2.31.0", Ecosystem: "pypi", Source: "requirements.txt", License: "MIT"},
		{Name: "flask", Version: "3.0.0", Ecosystem: "pypi", Source: "requirements.txt"},
	}
	out := string(Render(deps, nil, "1.0.0"))
	// The licensed component must carry licenses[0].license.id.
	if !strings.Contains(out, `"id": "MIT"`) {
		t.Errorf("BOM missing license id MIT:\n%s", out)
	}
	// The unlicensed component must not carry a licenses key at all.
	// Verify by parsing and checking per-component.
	var doc struct {
		Components []struct {
			Name     string            `json:"name"`
			Licenses []json.RawMessage `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid CycloneDX JSON: %v\n%s", err, out)
	}
	for _, c := range doc.Components {
		switch c.Name {
		case "requests":
			if len(c.Licenses) != 1 {
				t.Errorf("requests: want 1 license entry, got %d", len(c.Licenses))
			}
		case "flask":
			if len(c.Licenses) != 0 {
				t.Errorf("flask: want no licenses array, got %d entries", len(c.Licenses))
			}
		}
	}
}
