package analysis

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverSkillDependencies parses the dependency manifests bundled inside each
// discovered skill into a flat, sorted, de-duplicated BOM (Story D / TR-221). It
// reads only files already inventoried as a skill's BundledFiles —
// requirements.txt (pip) and package.json (npm) — and records the dependencies
// they declare. It does NO network access and NO CVE matching: this is a
// deterministic hand-off to a real SCA tool (OSV-Scanner, Dependabot), not a
// vulnerability verdict.
//
// pyproject.toml is intentionally NOT parsed yet: the engine ships no TOML
// parser, and a text-scan of PEP 621 / Poetry dependency tables is too fragile
// to ship as authoritative inventory. That is a known v1 gap (tracked for a
// follow-up), called out in COVERAGE.md so the omission is honest rather than
// silent.
func DiscoverSkillDependencies(skills []models.SkillDef, repoRoot string) []models.DepRef {
	var out []models.DepRef
	for _, s := range skills {
		for _, b := range s.BundledFiles {
			abs := filepath.Join(repoRoot, filepath.FromSlash(b.Path))
			switch strings.ToLower(filepath.Base(b.Path)) {
			case "requirements.txt":
				out = append(out, parseRequirementsTxt(abs, b.Path)...)
			case "package.json":
				out = append(out, parsePackageJSON(abs, b.Path)...)
			}
		}
	}
	return dedupeDeps(out)
}

// requirementLineRe captures a PEP 508 requirement's distribution name (group 1)
// and its optional version specifier (group 2, which begins at the first
// operator character). Extras (`[...]`) and environment markers (`; ...`) are
// matched but not captured.
var requirementLineRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*(?:\[[^\]]*\])?\s*([=<>!~][^;#]*)?`)

// parseRequirementsTxt extracts pip dependencies from a requirements.txt. It
// skips comments, blank lines, pip option lines (-r/-e/-c/--hash/...), and
// VCS/URL/local installs, which carry no clean name@version pair.
func parseRequirementsTxt(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var out []models.DepRef
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, ".") ||
			strings.HasPrefix(line, "/") || strings.HasPrefix(line, "git+") ||
			strings.Contains(line, "://") {
			continue
		}
		m := requirementLineRe.FindStringSubmatch(line)
		if m == nil || m[1] == "" {
			continue
		}
		// Strip a leading `==` so a pin renders as a bare version ("2.0"),
		// matching npm's exact-version form; ranges keep their operators
		// (">=1.0,<2.0", "~=4.1").
		version := strings.TrimSpace(m[2])
		version = strings.TrimSpace(strings.TrimPrefix(version, "=="))
		out = append(out, models.DepRef{
			Name:      m[1],
			Version:   version,
			Ecosystem: "pypi",
			Source:    source,
		})
	}
	return out
}

// parsePackageJSON extracts npm dependencies (runtime + dev) from a package.json.
// Version strings are stored verbatim (a range like "^1.0.0", an exact "1.2.3",
// or a non-registry spec like "github:org/repo"). A malformed package.json is
// skipped rather than failing the scan.
func parsePackageJSON(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &pkg); err != nil {
		return nil
	}
	var out []models.DepRef
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name, version := range deps {
			if strings.TrimSpace(name) == "" {
				continue
			}
			out = append(out, models.DepRef{
				Name:      name,
				Version:   strings.TrimSpace(version),
				Ecosystem: "npm",
				Source:    source,
			})
		}
	}
	return out
}

// dedupeDeps returns a deterministically ordered, de-duplicated copy: sorted by
// (ecosystem, name, version, source) with exact-duplicate DepRefs collapsed. The
// sort is what makes the BOM byte-stable regardless of manifest line order or Go
// map iteration order (package.json deps are decoded into a map).
func dedupeDeps(in []models.DepRef) []models.DepRef {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		a, b := in[i], in[j]
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
	out := make([]models.DepRef, 0, len(in))
	for i, d := range in {
		if i > 0 && d == in[i-1] {
			continue
		}
		out = append(out, d)
	}
	return out
}
