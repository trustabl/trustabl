package rulesource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

func TestReleaseURLs(t *testing.T) {
	const repo = "https://github.com/trustabl/trustabl-rules"
	if got, want := repoWebBase(repo+".git"), repo; got != want {
		t.Errorf("repoWebBase trims .git: got %q want %q", got, want)
	}
	if got, want := repoWebBase(repo+"/"), repo; got != want {
		t.Errorf("repoWebBase trims slash: got %q want %q", got, want)
	}
	if got, want := statementURL(repo, "production"), repo+"/releases/download/channel-production/statement.json"; got != want {
		t.Errorf("statementURL: got %q want %q", got, want)
	}
	if got, want := bundleURL(repo, "abc123"), repo+"/releases/download/bundle-abc123/bundle.tar.gz"; got != want {
		t.Errorf("bundleURL: got %q want %q", got, want)
	}
}

type fakeGetter struct {
	resp map[string][]byte // url -> 200 body; absent => 404
}

func (f *fakeGetter) Get(url string) (*http.Response, error) {
	if body, ok := f.resp[url]; ok {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body))}, nil
	}
	return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(""))}, nil
}

func gzTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGitHubTransport_FetchBundle(t *testing.T) {
	const repo = "https://example.com/rules"
	body := gzTar(t, map[string]string{
		"manifest.yaml":        "schema_version: 9\n",
		"claude_sdk/pack.yaml": "rules: []\n",
	})
	tr := &githubTransport{client: &fakeGetter{resp: map[string][]byte{bundleURL(repo, "d1"): body}}}

	fsys, err := tr.FetchBundle(repo, "d1")
	if err != nil {
		t.Fatalf("FetchBundle: %v", err)
	}
	if b, err := fs.ReadFile(fsys, "manifest.yaml"); err != nil || !strings.Contains(string(b), "schema_version") {
		t.Errorf("unpacked bundle missing manifest.yaml: %v", err)
	}
	if _, err := fs.ReadFile(fsys, "claude_sdk/pack.yaml"); err != nil {
		t.Errorf("unpacked bundle missing nested file: %v", err)
	}
}

func TestGitHubTransport_FetchStatement(t *testing.T) {
	const repo = "https://example.com/rules"
	tr := &githubTransport{client: &fakeGetter{resp: map[string][]byte{statementURL(repo, "production"): []byte(`{"hello":"world"}`)}}}
	got, err := tr.FetchStatement(repo, "production")
	if err != nil {
		t.Fatalf("FetchStatement: %v", err)
	}
	if string(got) != `{"hello":"world"}` {
		t.Errorf("FetchStatement body = %q", got)
	}
}

func TestGitHubTransport_Non200(t *testing.T) {
	tr := &githubTransport{client: &fakeGetter{}} // every URL 404s
	if _, err := tr.FetchStatement("https://example.com/rules", "production"); err == nil {
		t.Fatal("want error on 404")
	}
}

func TestUntarGz_RejectsUnsafePath(t *testing.T) {
	body := gzTar(t, map[string]string{"../escape.yaml": "x"})
	if _, err := untarGz(body); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("want unsafe-path error, got %v", err)
	}
}

func TestUntarGz_RejectsEmpty(t *testing.T) {
	body := gzTar(t, map[string]string{})
	if _, err := untarGz(body); err == nil {
		t.Fatal("want error for empty bundle")
	}
}
