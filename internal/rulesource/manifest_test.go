package rulesource

import (
	"testing"
	"testing/fstest"
)

func TestReadManifestInfo(t *testing.T) {
	cases := []struct {
		name        string
		fsys        fstest.MapFS
		wantValid   bool
		wantVersion int
	}{
		{"version 1", fstest.MapFS{"manifest.yaml": &fstest.MapFile{Data: []byte("schema_version: 1\n")}}, true, 1},
		{"version 3", fstest.MapFS{"manifest.yaml": &fstest.MapFile{Data: []byte("schema_version: 3\n")}}, true, 3},
		{"missing manifest", fstest.MapFS{}, false, 0},
		{"malformed manifest", fstest.MapFS{"manifest.yaml": &fstest.MapFile{Data: []byte("not: [valid")}}, false, 0},
		{"zero version", fstest.MapFS{"manifest.yaml": &fstest.MapFile{Data: []byte("schema_version: 0\n")}}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readManifestInfo(tc.fsys)
			if got.valid != tc.wantValid {
				t.Errorf("readManifestInfo valid = %v, want %v", got.valid, tc.wantValid)
			}
			if tc.wantValid && got.version != tc.wantVersion {
				t.Errorf("readManifestInfo version = %d, want %d", got.version, tc.wantVersion)
			}
		})
	}
}

// TestReadManifestInfo_NewerIsValid is the regression guard for the
// forward-compatibility change: a pack whose schema_version exceeds the
// engine's supported version is now VALID (usable), not rejected. usePack flags
// it newer via version > supported; the lenient loader then skips the
// individual rules this build cannot evaluate.
func TestReadManifestInfo_NewerIsValid(t *testing.T) {
	fsys := fstest.MapFS{"manifest.yaml": &fstest.MapFile{Data: []byte("schema_version: 9\n")}}
	got := readManifestInfo(fsys)
	if !got.valid {
		t.Fatal("a newer pack must be valid (usable), got valid=false")
	}
	const supported = 8
	if !(got.version > supported) {
		t.Fatalf("version %d should be newer than supported %d", got.version, supported)
	}
}
