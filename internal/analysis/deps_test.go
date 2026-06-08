package analysis

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func writeDep(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// depKeys flattens deps to sorted "ecosystem:name@version" keys for comparison.
func depKeys(deps []models.DepRef) []string {
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Ecosystem+":"+d.Name+"@"+d.Version)
	}
	sort.Strings(out)
	return out
}

// TestDiscoverDependencies_AllEcosystems exercises every supported manifest in
// one repo, plus the vendored-dir skip and the go.mod indirect filter.
func TestDiscoverDependencies_AllEcosystems(t *testing.T) {
	root := t.TempDir()
	writeDep(t, root, "requirements.txt", "requests==2.31.0\nflask>=1.0\n# c\n-e .\nhttps://x/y.whl\n")
	writeDep(t, root, "pyproject.toml", "[project]\nname = 'x'\ndependencies = [\"httpx>=0.24\", \"pydantic==2.5.0\"]\n")
	writeDep(t, root, "svc/package.json", `{"dependencies":{"lodash":"^4.17.21"},"devDependencies":{"jest":"29.0.0"}}`)
	writeDep(t, root, "composer.json", `{"require":{"monolog/monolog":"^2.0","php":">=7.4","ext-json":"*"},"require-dev":{"phpunit/phpunit":"^9"}}`)
	writeDep(t, root, "go.mod", "module x\n\ngo 1.21\n\nrequire (\n\tgithub.com/foo/bar v1.2.3\n\tgithub.com/baz/qux v0.4.0 // indirect\n)\n\nrequire github.com/single/dep v1.0.0\n")
	writeDep(t, root, "rustsvc/Cargo.toml", "[package]\nname = \"x\"\n[dependencies]\nserde = \"1.0\"\ntokio = { version = \"1.35\", features = [\"full\"] }\n")
	writeDep(t, root, "App.csproj", `<Project><ItemGroup><PackageReference Include="Newtonsoft.Json" Version="13.0.1" /><PackageReference Include="Serilog"><Version>2.10.0</Version></PackageReference></ItemGroup></Project>`)
	// vendored / installed trees must be skipped (they carry installed manifests).
	writeDep(t, root, "node_modules/evil/package.json", `{"dependencies":{"should-not-appear":"1.0.0"}}`)
	writeDep(t, root, "vendor/x/go.mod", "module y\nrequire github.com/should/not v9.9.9\n")

	got := depKeys(DiscoverDependencies(root))
	want := []string{
		"cargo:serde@1.0",
		"cargo:tokio@1.35",
		"composer:monolog/monolog@^2.0",
		"composer:phpunit/phpunit@^9",
		"golang:github.com/foo/bar@v1.2.3",
		"golang:github.com/single/dep@v1.0.0",
		"npm:jest@29.0.0",
		"npm:lodash@^4.17.21",
		"nuget:Newtonsoft.Json@13.0.1",
		"nuget:Serilog@2.10.0",
		"pypi:flask@>=1.0",
		"pypi:httpx@>=0.24",
		"pypi:pydantic@2.5.0",
		"pypi:requests@2.31.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverDependencies mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestDiscoverDependencies_PoetryAndPipfile(t *testing.T) {
	root := t.TempDir()
	writeDep(t, root, "pyproject.toml", "[tool.poetry.dependencies]\npython = \"^3.10\"\nrequests = \"^2.31\"\n[tool.poetry.group.dev.dependencies]\npytest = \"^8.0\"\n")
	writeDep(t, root, "svc/Pipfile", "[packages]\nflask = \"*\"\nboto3 = \"==1.34.0\"\n[dev-packages]\nmypy = \"*\"\n")
	got := depKeys(DiscoverDependencies(root))
	want := []string{
		"pypi:boto3@==1.34.0",
		"pypi:flask@", // "*" normalized to empty
		"pypi:mypy@",  // "*" normalized to empty
		"pypi:pytest@^8.0",
		"pypi:requests@^2.31", // "python" interpreter constraint excluded
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("poetry/pipfile mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestDiscoverDependencies_Empty(t *testing.T) {
	root := t.TempDir()
	writeDep(t, root, "README.md", "# nothing here\n")
	if got := DiscoverDependencies(root); got != nil {
		t.Errorf("a repo with no manifests should yield nil, got %+v", got)
	}
}
