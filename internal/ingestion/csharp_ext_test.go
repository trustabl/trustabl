package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestNormalize_CollectsCSharpFiles verifies .cs sources land in CSharpFiles and
// that .csproj / .csv are not misclassified as C# source.
func TestNormalize_CollectsCSharpFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Tools.cs", "Program.cs", "App.csproj", "data.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.CSharpFiles) != 2 {
		t.Errorf("expected 2 C# files (Tools.cs, Program.cs), got %d: %v", len(m.CSharpFiles), m.CSharpFiles)
	}
	var hasCSharp bool
	for _, l := range languagesFromManifest(m) {
		if l == models.LanguageCSharp {
			hasCSharp = true
		}
	}
	if !hasCSharp {
		t.Errorf("languagesFromManifest missing csharp: %v", languagesFromManifest(m))
	}
}
