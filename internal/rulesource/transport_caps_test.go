package rulesource

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// bodyGetter returns an httpGetter that serves a fixed 200 body for any URL.
func bodyGetter(s string) httpGetter { return fixedBody{s} }

type fixedBody struct{ body string }

func (g fixedBody) Get(string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(g.body)),
	}, nil
}

// TestTransport_StatementByteCeiling: a statement body over maxStatementBytes is
// rejected (unbounded-body defense); a body exactly at the cap succeeds.
func TestTransport_StatementByteCeiling(t *testing.T) {
	old := maxStatementBytes
	maxStatementBytes = 16
	t.Cleanup(func() { maxStatementBytes = old })

	if _, err := (&githubTransport{client: bodyGetter(strings.Repeat("a", 17))}).FetchStatement("https://x/r", "production"); err == nil {
		t.Fatal("over-cap statement body was accepted")
	}
	if _, err := (&githubTransport{client: bodyGetter(strings.Repeat("a", 16))}).FetchStatement("https://x/r", "production"); err != nil {
		t.Fatalf("at-cap statement body should succeed: %v", err)
	}
}

// TestUntarGz_EntryCeiling: more than maxBundleEntries regular files is rejected.
func TestUntarGz_EntryCeiling(t *testing.T) {
	old := maxBundleEntries
	maxBundleEntries = 2
	t.Cleanup(func() { maxBundleEntries = old })

	files := map[string]string{"manifest.yaml": "schema_version: 12\n", "a.yaml": "x", "b.yaml": "y"}
	if _, err := untarGz(gzTar(t, files)); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("over-entry-cap bundle: want an 'entries' error, got %v", err)
	}
}

// TestUntarGz_UnpackedCeiling: total unpacked size over maxBundleUnpacked is
// rejected; a just-at-cap bundle succeeds (locks the +1 boundary).
func TestUntarGz_UnpackedCeiling(t *testing.T) {
	old := maxBundleUnpacked
	maxBundleUnpacked = 8
	t.Cleanup(func() { maxBundleUnpacked = old })

	if _, err := untarGz(gzTar(t, map[string]string{"a.yaml": strings.Repeat("x", 9)})); err == nil || !strings.Contains(err.Error(), "unpacked") {
		t.Fatalf("over-unpacked-cap bundle: want an 'unpacked' error, got %v", err)
	}
	if _, err := untarGz(gzTar(t, map[string]string{"a.yaml": strings.Repeat("x", 8)})); err != nil {
		t.Fatalf("at-unpacked-cap bundle should succeed: %v", err)
	}
}
