package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// TestNormalize_CollectsRustFiles verifies .rs sources land in RustFiles and that
// Cargo.toml is not misclassified as Rust source.
func TestNormalize_CollectsRustFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tools.rs", "server.rs", "Cargo.toml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.RustFiles) != 2 {
		t.Errorf("expected 2 Rust files (tools.rs, server.rs), got %d: %v", len(m.RustFiles), m.RustFiles)
	}
	var hasRust bool
	for _, l := range languagesFromManifest(m) {
		if l == models.LanguageRust {
			hasRust = true
		}
	}
	if !hasRust {
		t.Errorf("languagesFromManifest missing rust: %v", languagesFromManifest(m))
	}
}
