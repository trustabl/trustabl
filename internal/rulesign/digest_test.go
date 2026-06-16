package rulesign_test

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// goldenFixture is a fixed rule-pack-shaped tree. Its digest is frozen below so
// any future change to the canonical-tar serialization fails CI loudly rather
// than silently shifting the bundle identity under already-shipped binaries.
func goldenFixture() fstest.MapFS {
	return fstest.MapFS{
		"manifest.yaml":         {Data: []byte("schema_version: 12\n")},
		"claude_sdk/tool.yaml":  {Data: []byte("id: CSDK-010\nseverity: high\n")},
		"openai_sdk/agent.yaml": {Data: []byte("id: OAI-105\nseverity: medium\n")},
		"openai_sdk/tool.yaml":  {Data: []byte("id: OAI-016\nseverity: low\n")},
		"nested/deep/rule.yaml": {Data: []byte("id: MCP-001\n")},
		"emptydir/.placeholder": {Data: []byte("")},
	}
}

// readTar reads a (non-gzipped) tar into an in-memory FS, mirroring what the
// engine's bundle transport does after gunzip — so a round-trip through
// WriteCanonicalTar can be re-digested and compared.
func readTar(t *testing.T, raw []byte) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %q: %v", hdr.Name, err)
		}
		out[hdr.Name] = &fstest.MapFile{Data: data}
	}
	return out
}

// TestWriteCanonicalTar_RoundTripsToSameDigest is the producer/consumer contract:
// a bundle written by WriteCanonicalTar, when read back and re-digested, must
// yield the exact CanonicalDigest of the source tree. This is what lets a
// publisher (RUL-4) build the bundle the engine will independently verify.
func TestWriteCanonicalTar_RoundTripsToSameDigest(t *testing.T) {
	src := goldenFixture()
	want, err := rulesign.CanonicalDigest(src)
	if err != nil {
		t.Fatalf("CanonicalDigest(src): %v", err)
	}

	var buf bytes.Buffer
	if err := rulesign.WriteCanonicalTar(&buf, src); err != nil {
		t.Fatalf("WriteCanonicalTar: %v", err)
	}

	got, err := rulesign.CanonicalDigest(readTar(t, buf.Bytes()))
	if err != nil {
		t.Fatalf("CanonicalDigest(round-trip): %v", err)
	}
	if got != want {
		t.Fatalf("round-trip digest = %s, want %s", got, want)
	}
}

// TestWriteCanonicalTar_Deterministic asserts the artifact bytes themselves are
// a pure function of (paths × contents): two writes of the same tree are
// byte-identical, so a re-publish of unchanged rules produces the same
// bundle-<digest> and the release upload is idempotent.
func TestWriteCanonicalTar_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := rulesign.WriteCanonicalTar(&a, goldenFixture()); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := rulesign.WriteCanonicalTar(&b, goldenFixture()); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("WriteCanonicalTar output is not byte-stable across runs")
	}
}

// TestWriteCanonicalTar_RejectsBackslashPath: a path containing a backslash is a
// legal byte in a slash-separated fs path, but on Windows it reinterprets as a
// separator, so the same signed bundle would install to different files on
// Windows vs Linux. The producer must refuse to sign such a path.
func TestWriteCanonicalTar_RejectsBackslashPath(t *testing.T) {
	src := fstest.MapFS{
		"manifest.yaml":     {Data: []byte("schema_version: 12\n")},
		"config\\prod.yaml": {Data: []byte("id: X\n")},
	}
	var buf bytes.Buffer
	if err := rulesign.WriteCanonicalTar(&buf, src); err == nil {
		t.Fatal("WriteCanonicalTar accepted a backslash-containing path")
	}
	if _, err := rulesign.CanonicalDigest(src); err == nil {
		t.Fatal("CanonicalDigest accepted a backslash-containing path")
	}
}

// TestValidateBundlePath rejects names fs.ValidPath accepts but that reinterpret
// across platforms, so the digest (over the in-memory set) always equals the
// on-disk tree the engine installs.
func TestValidateBundlePath(t *testing.T) {
	bad := []string{
		`config\prod.yaml`, // backslash = separator on Windows
		"c:/evil.yaml",     // drive-letter / colon
		"c:evil.yaml",      // drive-relative
		"a/b:c.yaml",       // colon mid-path (illegal on Windows)
		"manifest.yaml.",   // trailing dot (Windows strips)
		"manifest.yaml ",   // trailing space (Windows strips)
		"claude_sdk/CON",   // reserved device name
		"prn.yaml",         // reserved device name + ext
		"COM1.txt",         // reserved COMn
		"lpt9",             // reserved LPTn (lowercase)
		"a\x01b.yaml",      // control character
	}
	for _, name := range bad {
		if err := rulesign.ValidateBundlePath(name); err == nil {
			t.Errorf("ValidateBundlePath(%q) = nil, want rejection", name)
		}
	}
	good := []string{
		"manifest.yaml",
		"claude_sdk/tool.yaml",
		"openai_sdk/agent.yaml",
		"nested/deep/rule.yaml",
		"emptydir/.placeholder",
		"a.b.c/d-e_f.yaml",
		"COMPANION.yaml", // not a reserved name (COM + more than one trailing char)
	}
	for _, name := range good {
		if err := rulesign.ValidateBundlePath(name); err != nil {
			t.Errorf("ValidateBundlePath(%q) = %v, want nil", name, err)
		}
	}
}

// TestWriteCanonicalTar_RejectsCaseFoldCollision: two paths that differ only by
// case collapse to one file on a case-insensitive FS, so the on-disk tree would
// have fewer files than the verified digest covers. Refuse at the source.
func TestWriteCanonicalTar_RejectsCaseFoldCollision(t *testing.T) {
	src := fstest.MapFS{
		"manifest.yaml":     {Data: []byte("schema_version: 12\n")},
		"claude_sdk/a.yaml": {Data: []byte("id: A\n")},
		"claude_sdk/A.yaml": {Data: []byte("id: B\n")},
	}
	if _, err := rulesign.CanonicalDigest(src); err == nil {
		t.Fatal("CanonicalDigest accepted case-folding-colliding paths")
	}
}

// TestCanonicalDigest_GoldenFreeze pins the canonical digest of a fixed tree.
// If this constant ever has to change, the canonical-tar encoding changed, which
// breaks digest agreement with every field binary built before the change — so
// this test failing is a deliberate, breaking-change gate, not a nuisance.
func TestCanonicalDigest_GoldenFreeze(t *testing.T) {
	got, err := rulesign.CanonicalDigest(goldenFixture())
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	const want = "c198c970a4546eb2b5d379df0a1860e6b153ed99f34e9efb3fcc4afff2be3c66"
	if got != want {
		t.Fatalf("canonical digest = %s, want %s (if this changed intentionally, "+
			"the bundle encoding is a breaking change — see the test comment)", got, want)
	}
}
