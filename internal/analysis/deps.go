package analysis

import (
	"encoding/json"
	"encoding/xml"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverDependencies walks the repo for the primary dependency manifest of
// each supported language and parses every declared DIRECT dependency into a
// flat, sorted, de-duplicated BOM (Story TR-278; supersedes the skill-only BOM
// of TR-221). It is pure inventory — no network, no CVE matching — a
// deterministic hand-off to a real SCA tool (OSV-Scanner, Dependabot, syft). It
// reports DECLARED deps from manifests, never transitive lockfile resolution.
//
// Vendored / installed dependency trees (node_modules, vendor, .venv, target, …)
// are skipped, so the BOM lists what the repo DECLARES, not the thousands of
// manifests its already-installed packages ship. Deps a skill bundles inside its
// own directory are still captured (the walk descends into skill dirs) and stay
// attributable via DepRef.Source.
func DiscoverDependencies(repoRoot string) []models.DepRef {
	var out []models.DepRef
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != repoRoot && skipForDeps(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch base := d.Name(); {
		case base == "requirements.txt":
			out = append(out, parsePyRequirements(path, rel)...)
		case base == "pyproject.toml":
			out = append(out, parsePyproject(path, rel)...)
		case base == "Pipfile":
			out = append(out, parsePipfile(path, rel)...)
		case base == "package.json":
			out = append(out, parseNpmPackageJSON(path, rel)...)
		case base == "composer.json":
			out = append(out, parseComposerJSON(path, rel)...)
		case base == "go.mod":
			out = append(out, parseGoMod(path, rel)...)
		case base == "Cargo.toml":
			out = append(out, parseCargoToml(path, rel)...)
		case strings.HasSuffix(base, ".csproj"):
			out = append(out, parseCsproj(path, rel)...)
		}
		return nil
	})
	return dedupeDeps(out)
}

// skipForDeps reports whether a directory must not be descended into when
// looking for DECLARED manifests: VCS, virtualenvs, IDE state, and
// installed/vendored dependency trees (which carry their packages' own
// manifests — installed, not declared).
func skipForDeps(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".venv", "venv", "env", "node_modules", "vendor",
		"__pycache__", "dist", "build", "target", "bin", "obj", ".tox",
		".mypy_cache", ".pytest_cache", ".gradle", ".idea", ".vscode":
		return true
	}
	return false
}

// requirementLineRe captures a PEP 508 requirement's distribution name (group 1)
// and its optional version specifier (group 2, from the first operator char).
var requirementLineRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*(?:\[[^\]]*\])?\s*([=<>!~][^;#]*)?`)

// pypiSpec turns a PEP 508 requirement string into a DepRef, stripping a leading
// `==` so a pin renders as a bare version (matching npm's exact form).
func pypiSpec(spec, source string) *models.DepRef {
	m := requirementLineRe.FindStringSubmatch(strings.TrimSpace(spec))
	if m == nil || m[1] == "" {
		return nil
	}
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[2]), "=="))
	return &models.DepRef{Name: m[1], Version: v, Ecosystem: "pypi", Source: source}
}

// parsePyRequirements extracts pip deps from a requirements.txt, skipping
// comments, option lines (-r/-e/--hash/…), and VCS/URL/local installs.
func parsePyRequirements(abs, source string) []models.DepRef {
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
		if d := pypiSpec(line, source); d != nil {
			out = append(out, *d)
		}
	}
	return withLines(out, content)
}

// parsePyproject extracts pip deps from a pyproject.toml — both the PEP 621
// `[project] dependencies` / `optional-dependencies` (PEP 508 strings) and the
// Poetry `[tool.poetry] dependencies` / `dev-dependencies` / `group.*` (maps).
func parsePyproject(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var doc struct {
		Project struct {
			Dependencies         []string            `toml:"dependencies"`
			OptionalDependencies map[string][]string `toml:"optional-dependencies"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies    map[string]any `toml:"dependencies"`
				DevDependencies map[string]any `toml:"dev-dependencies"`
				Group           map[string]struct {
					Dependencies map[string]any `toml:"dependencies"`
				} `toml:"group"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []models.DepRef
	for _, spec := range doc.Project.Dependencies {
		if d := pypiSpec(spec, source); d != nil {
			out = append(out, *d)
		}
	}
	for _, specs := range doc.Project.OptionalDependencies {
		for _, spec := range specs {
			if d := pypiSpec(spec, source); d != nil {
				out = append(out, *d)
			}
		}
	}
	addPoetry := func(m map[string]any) {
		for name, v := range m {
			if name == "python" { // the interpreter constraint, not a package
				continue
			}
			out = append(out, models.DepRef{Name: name, Version: tomlVersion(v), Ecosystem: "pypi", Source: source})
		}
	}
	addPoetry(doc.Tool.Poetry.Dependencies)
	addPoetry(doc.Tool.Poetry.DevDependencies)
	for _, g := range doc.Tool.Poetry.Group {
		addPoetry(g.Dependencies)
	}
	return withLines(out, content)
}

// parsePipfile extracts pip deps from a Pipfile ([packages] / [dev-packages]).
func parsePipfile(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var doc struct {
		Packages    map[string]any `toml:"packages"`
		DevPackages map[string]any `toml:"dev-packages"`
	}
	if err := toml.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []models.DepRef
	add := func(m map[string]any) {
		for name, v := range m {
			ver := tomlVersion(v)
			if ver == "*" {
				ver = ""
			}
			out = append(out, models.DepRef{Name: name, Version: ver, Ecosystem: "pypi", Source: source})
		}
	}
	add(doc.Packages)
	add(doc.DevPackages)
	return withLines(out, content)
}

// parseNpmPackageJSON extracts npm deps (runtime + dev) from a package.json.
func parseNpmPackageJSON(abs, source string) []models.DepRef {
	return parseJSONDepMaps(abs, source, "npm",
		[]string{"dependencies", "devDependencies"}, nil)
}

// parseComposerJSON extracts PHP deps from a composer.json (require + require-dev).
// Platform/meta requirements (php, ext-*, anything without a vendor/name slash)
// are skipped — they are not Packagist packages.
func parseComposerJSON(abs, source string) []models.DepRef {
	return parseJSONDepMaps(abs, source, "composer",
		[]string{"require", "require-dev"},
		func(name string) bool { return !strings.Contains(name, "/") })
}

// parseJSONDepMaps reads the named top-level objects of a JSON manifest
// (package.json / composer.json) as name->version-constraint maps.
func parseJSONDepMaps(abs, source, ecosystem string, keys []string, skip func(string) bool) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil
	}
	var out []models.DepRef
	for _, k := range keys {
		blob, ok := raw[k]
		if !ok {
			continue
		}
		var deps map[string]string
		if err := json.Unmarshal(blob, &deps); err != nil {
			continue
		}
		for name, ver := range deps {
			name = strings.TrimSpace(name)
			if name == "" || (skip != nil && skip(name)) {
				continue
			}
			out = append(out, models.DepRef{Name: name, Version: strings.TrimSpace(ver), Ecosystem: ecosystem, Source: source})
		}
	}
	return withLines(out, content)
}

// goRequireRe captures a go.mod require entry: module path (1) + version (2).
var goRequireRe = regexp.MustCompile(`^([^\s]+)\s+(v[^\s]+)`)

// parseGoMod extracts the DIRECT deps from a go.mod (skipping `// indirect`).
func parseGoMod(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var out []models.DepRef
	inBlock := false
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		indirect := strings.Contains(line, "// indirect")
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case inBlock && line == ")":
			inBlock = false
		case line == "require (":
			inBlock = true
		case inBlock:
			if !indirect {
				if m := goRequireRe.FindStringSubmatch(line); m != nil {
					out = append(out, models.DepRef{Name: m[1], Version: m[2], Ecosystem: "golang", Source: source})
				}
			}
		case strings.HasPrefix(line, "require ") && !indirect:
			if m := goRequireRe.FindStringSubmatch(strings.TrimSpace(line[len("require "):])); m != nil {
				out = append(out, models.DepRef{Name: m[1], Version: m[2], Ecosystem: "golang", Source: source})
			}
		}
	}
	return withLines(out, content)
}

// parseCargoToml extracts Rust deps from a Cargo.toml ([dependencies],
// [dev-dependencies], [build-dependencies]). A dep value is either a version
// string or an inline table { version = "…", … }.
func parseCargoToml(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	var doc struct {
		Dependencies      map[string]any `toml:"dependencies"`
		DevDependencies   map[string]any `toml:"dev-dependencies"`
		BuildDependencies map[string]any `toml:"build-dependencies"`
	}
	if err := toml.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []models.DepRef
	add := func(m map[string]any) {
		for name, v := range m {
			out = append(out, models.DepRef{Name: name, Version: tomlVersion(v), Ecosystem: "cargo", Source: source})
		}
	}
	add(doc.Dependencies)
	add(doc.DevDependencies)
	add(doc.BuildDependencies)
	return withLines(out, content)
}

// parseCsproj extracts NuGet deps from a .csproj <PackageReference> elements
// (Version as an attribute or a child <Version> element), at any nesting depth.
func parseCsproj(abs, source string) []models.DepRef {
	content, err := readCapped(abs, maxBundledScriptScanBytes)
	if err != nil {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(string(content)))
	var out []models.DepRef
	for {
		tok, terr := dec.Token()
		if terr != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "PackageReference" {
			continue
		}
		var pr struct {
			Include string `xml:"Include,attr"`
			VerAttr string `xml:"Version,attr"`
			VerEl   string `xml:"Version"`
		}
		if err := dec.DecodeElement(&pr, &se); err != nil {
			continue
		}
		if strings.TrimSpace(pr.Include) == "" {
			continue
		}
		v := pr.VerAttr
		if v == "" {
			v = pr.VerEl
		}
		out = append(out, models.DepRef{Name: pr.Include, Version: strings.TrimSpace(v), Ecosystem: "nuget", Source: source})
	}
	return withLines(out, content)
}

// tomlVersion extracts a version constraint from a TOML dependency value that is
// either a bare string ("1.0") or an inline table with a `version` key
// ({ version = "1", features = […] }).
func tomlVersion(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		if s, ok := t["version"].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// depNameChar reports whether b can appear inside a dependency-name token. Used
// by lineOf to require a whole-token match so "requests" is not located inside
// "requests-oauthlib".
func depNameChar(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '.', '_', '-', '/', '@', '+':
		return true
	}
	return false
}

// lineOf returns the 1-indexed line of the first whole-token occurrence of name
// in content, or 0 if absent. Whole-token = not flanked by name characters, so a
// declaration line is matched rather than a substring of a longer dependency
// name. This recovers the line for manifests parsed by a TOML / JSON / XML
// library that exposes no source positions, and works equally for the line-based
// formats (requirements.txt, go.mod).
func lineOf(content []byte, name string) int {
	if name == "" {
		return 0
	}
	for i, line := range strings.Split(string(content), "\n") {
		from := 0
		for {
			j := strings.Index(line[from:], name)
			if j < 0 {
				break
			}
			j += from
			beforeOK := j == 0 || !depNameChar(line[j-1])
			after := j + len(name)
			afterOK := after >= len(line) || !depNameChar(line[after])
			if beforeOK && afterOK {
				return i + 1
			}
			from = j + 1
		}
	}
	return 0
}

// withLines stamps each dep's 1-indexed declaration line by locating its name in
// the manifest content. A declaration occupies a single line, so EndLine ==
// StartLine. A name that cannot be located falls back to line 1 so a dependency
// is never reported at line 0.
func withLines(deps []models.DepRef, content []byte) []models.DepRef {
	for i := range deps {
		ln := lineOf(content, deps[i].Name)
		if ln == 0 {
			ln = 1
		}
		deps[i].StartLine = ln
		deps[i].EndLine = ln
	}
	return deps
}

// dedupeDeps returns a deterministically ordered, de-duplicated copy: sorted by
// (ecosystem, name, version, source) with exact-duplicate DepRefs collapsed. The
// sort makes the BOM byte-stable regardless of walk order or map iteration order.
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
