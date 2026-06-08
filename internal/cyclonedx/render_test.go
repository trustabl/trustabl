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
	a := Render(deps, "1.2.3")
	shuffled := []models.DepRef{deps[2], deps[0], deps[1]}
	b := Render(shuffled, "1.2.3")
	if !bytes.Equal(a, b) {
		t.Fatalf("Render not order-independent:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// TestRender_NoNondeterministicFields guards against a timestamp or random
// serial number leaking into the byte-stable output.
func TestRender_NoNondeterministicFields(t *testing.T) {
	out := string(Render(sampleDeps(), "1.2.3"))
	for _, banned := range []string{"timestamp", "serialNumber"} {
		if strings.Contains(out, banned) {
			t.Errorf("BOM contains nondeterministic field %q:\n%s", banned, out)
		}
	}
}

func TestRender_Structure(t *testing.T) {
	out := Render(sampleDeps(), "9.9.9")

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
	out := string(Render(nil, "1.0.0"))
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
	out := string(Render(deps, "1.0.0"))
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
