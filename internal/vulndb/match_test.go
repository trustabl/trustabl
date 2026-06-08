package vulndb

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// realisticRecords mirror the shape of actual OSV records across ecosystems —
// SEMVER + ECOSYSTEM ranges, an explicit versions[] list, and a withdrawn entry.
func realisticRecords() []Record {
	return []Record{
		{
			ID: "GHSA-jf85-cpcp-j695", Aliases: []string{"CVE-2019-10744"},
			Summary:  "Prototype Pollution in lodash",
			Severity: []Severity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}},
			Affected: []Affected{{Package: AffectedPackage{Ecosystem: "npm", Name: "lodash"},
				Ranges: []Range{{Type: "SEMVER", Events: []Event{{Introduced: "0"}, {Fixed: "4.17.12"}}}}}},
		},
		{
			ID: "PYSEC-2018-28", Aliases: []string{"CVE-2018-18074"},
			Summary:  "requests sends Authorization header on redirect",
			Severity: []Severity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N"}},
			Affected: []Affected{{Package: AffectedPackage{Ecosystem: "PyPI", Name: "Requests"}, // case/normalize test
				Ranges: []Range{{Type: "ECOSYSTEM", Events: []Event{{Introduced: "0"}, {Fixed: "2.20.0"}}}}}},
		},
		{
			ID:       "GO-2022-0001",
			Severity: []Severity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N"}},
			Affected: []Affected{{Package: AffectedPackage{Ecosystem: "Go", Name: "github.com/foo/bar"},
				Ranges: []Range{{Type: "SEMVER", Events: []Event{{Introduced: "0"}, {Fixed: "1.3.0"}}}}}},
		},
		{
			ID:       "RUSTSEC-2021-0001",
			Severity: []Severity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:L/I:N/A:N"}},
			Affected: []Affected{{Package: AffectedPackage{Ecosystem: "crates.io", Name: "badcrate"},
				Versions: []string{"0.1.0", "0.1.1"}}}, // explicit versions list
		},
		{
			ID: "WITHDRAWN-1", Withdrawn: "2021-01-01T00:00:00Z",
			Affected: []Affected{{Package: AffectedPackage{Ecosystem: "npm", Name: "left-pad"},
				Versions: []string{"1.0.0"}}},
		},
	}
}

func TestMatch_RealRecords(t *testing.T) {
	db := NewDB(realisticRecords())
	if db.Len() != 4 { // withdrawn excluded
		t.Errorf("DB.Len() = %d, want 4 (withdrawn excluded)", db.Len())
	}
	deps := []models.DepRef{
		{Name: "lodash", Version: "4.17.4", Ecosystem: "npm", Source: "package.json"},          // < 4.17.12 → vuln
		{Name: "lodash", Version: "4.17.21", Ecosystem: "npm", Source: "svc/package.json"},     // >= fixed → clean
		{Name: "requests", Version: "2.19.0", Ecosystem: "pypi", Source: "requirements.txt"},   // < 2.20.0 → vuln (PEP 440 + name-normalize)
		{Name: "requests", Version: "2.20.0", Ecosystem: "pypi", Source: "requirements.txt"},   // == fixed → clean
		{Name: "github.com/foo/bar", Version: "v1.2.3", Ecosystem: "golang", Source: "go.mod"}, // < 1.3.0 → vuln (v-prefix)
		{Name: "badcrate", Version: "0.1.1", Ecosystem: "cargo", Source: "Cargo.toml"},         // in versions[] → vuln
		{Name: "badcrate", Version: "0.2.0", Ecosystem: "cargo", Source: "Cargo.toml"},         // not listed → clean
		{Name: "lodash", Version: "^4.0.0", Ecosystem: "npm", Source: "x/package.json"},        // RANGE → skipped (not concrete)
		{Name: "left-pad", Version: "1.0.0", Ecosystem: "npm", Source: "package.json"},         // withdrawn record → no match
	}

	got := Match(deps, db)
	if len(got) != 4 {
		t.Fatalf("want 4 vulns, got %d: %+v", len(got), got)
	}
	by := map[string]models.DepVuln{}
	for _, v := range got {
		by[v.Dep.Name+"@"+v.Dep.Version] = v
	}

	if v := by["lodash@4.17.4"]; v.ID != "GHSA-jf85-cpcp-j695" || v.FixedIn != "4.17.12" || v.Severity != models.SeverityCritical {
		t.Errorf("lodash@4.17.4 = %+v; want GHSA-jf85-cpcp-j695, fixed 4.17.12, critical", v)
	}
	if v := by["requests@2.19.0"]; v.ID != "PYSEC-2018-28" || v.FixedIn != "2.20.0" {
		t.Errorf("requests@2.19.0 = %+v; want PYSEC-2018-28 fixed 2.20.0 (PEP 440 + name normalize)", v)
	}
	if v := by["github.com/foo/bar@v1.2.3"]; v.ID != "GO-2022-0001" || v.FixedIn != "1.3.0" {
		t.Errorf("go dep = %+v; want GO-2022-0001 fixed 1.3.0", v)
	}
	if v := by["badcrate@0.1.1"]; v.ID != "RUSTSEC-2021-0001" {
		t.Errorf("badcrate@0.1.1 = %+v; want RUSTSEC-2021-0001 (explicit versions[])", v)
	}
	for _, clean := range []string{"lodash@4.17.21", "requests@2.20.0", "badcrate@0.2.0", "lodash@^4.0.0", "left-pad@1.0.0"} {
		if _, bad := by[clean]; bad {
			t.Errorf("%s must NOT match", clean)
		}
	}
}
