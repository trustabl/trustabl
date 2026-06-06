package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestNormalize_CollectsGoFiles verifies .go sources land in GoFiles and that
// the go.mod / go.sum manifest files are NOT misclassified as Go source (they
// are dependency manifests, matched elsewhere).
func TestNormalize_CollectsGoFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"main.go", "tool.go", "go.mod", "go.sum"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.GoFiles) != 2 {
		t.Errorf("expected 2 Go files (main.go, tool.go), got %d: %v", len(m.GoFiles), m.GoFiles)
	}
	var hasGoLang bool
	for _, l := range languagesFromManifest(m) {
		if l == models.LanguageGo {
			hasGoLang = true
		}
	}
	if !hasGoLang {
		t.Errorf("languagesFromManifest missing go: %v", languagesFromManifest(m))
	}
}
