package rulesource

import (
	"strings"
	"testing"
)

// TestUntarGz_RejectsBackslashPath: a downloaded bundle whose entry name contains
// a backslash must be refused — on Windows it would reinterpret as a path
// separator, so the installed layout (and re-derived digest) would diverge from
// what the signed statement committed to. (gzTar lives in githubtransport_test.go.)
func TestUntarGz_RejectsBackslashPath(t *testing.T) {
	raw := gzTar(t, map[string]string{
		"manifest.yaml":     "schema_version: 12\n",
		"config\\prod.yaml": "id: X\n",
	})
	if _, err := untarGz(raw); err == nil {
		t.Fatal("untarGz accepted a backslash-containing bundle path")
	} else if !strings.Contains(err.Error(), "non-portable") {
		t.Fatalf("unexpected error %v, want a non-portable-path rejection", err)
	}

	// A clean bundle still unpacks.
	if _, err := untarGz(gzTar(t, map[string]string{"manifest.yaml": "schema_version: 12\n"})); err != nil {
		t.Fatalf("untarGz rejected a clean bundle: %v", err)
	}
}

// TestUntarGz_RejectsNonPortableAndCollisions: the consumer applies the same
// portability validator as the producer, and rejects case-fold collisions that
// would collapse two verified entries to one file on a case-insensitive FS.
func TestUntarGz_RejectsNonPortableAndCollisions(t *testing.T) {
	t.Run("reserved device name", func(t *testing.T) {
		if _, err := untarGz(gzTar(t, map[string]string{"manifest.yaml": "schema_version: 12\n", "claude_sdk/CON": "x"})); err == nil {
			t.Fatal("accepted a reserved device name")
		}
	})
	t.Run("trailing dot", func(t *testing.T) {
		if _, err := untarGz(gzTar(t, map[string]string{"manifest.yaml": "schema_version: 12\n", "a.yaml.": "x"})); err == nil {
			t.Fatal("accepted a trailing-dot path")
		}
	})
	t.Run("case-fold collision", func(t *testing.T) {
		if _, err := untarGz(gzTar(t, map[string]string{"manifest.yaml": "schema_version: 12\n", "p/a.yaml": "x", "p/A.yaml": "y"})); err == nil {
			t.Fatal("accepted case-folding-colliding paths")
		}
	})
}
