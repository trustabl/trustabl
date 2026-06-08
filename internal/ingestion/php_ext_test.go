package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestNormalize_CollectsPHPFiles verifies .php sources land in PHPFiles and that
// composer.json is not misclassified as PHP source.
func TestNormalize_CollectsPHPFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Tools.php", "Server.php", "composer.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.PHPFiles) != 2 {
		t.Errorf("expected 2 PHP files (Tools.php, Server.php), got %d: %v", len(m.PHPFiles), m.PHPFiles)
	}
	var hasPHP bool
	for _, l := range languagesFromManifest(m) {
		if l == models.LanguagePHP {
			hasPHP = true
		}
	}
	if !hasPHP {
		t.Errorf("languagesFromManifest missing php: %v", languagesFromManifest(m))
	}
}
