package ingestion

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalize_CollectsJavaScriptExtensions verifies all four JavaScript
// extensions land in JavaScriptFiles. .cjs was previously dropped from
// classification entirely; a file recon never classifies is a file the scanner
// never parses, so this guards the full JS extension set.
func TestNormalize_CollectsJavaScriptExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.js", "b.jsx", "c.mjs", "d.cjs"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{RootPath: dir}
	m, err := Normalize(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.JavaScriptFiles) != 4 {
		t.Errorf("expected 4 JS files (.js/.jsx/.mjs/.cjs), got %d: %v", len(m.JavaScriptFiles), m.JavaScriptFiles)
	}
}
