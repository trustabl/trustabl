package vulndb

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRecordsFromZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	add("GHSA-aaaa.json", `{"id":"GHSA-aaaa","affected":[{"package":{"ecosystem":"npm","name":"lodash"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"4.17.12"}]}]}]}`)
	add("PYSEC-bbbb.json", `{"id":"PYSEC-bbbb","affected":[{"package":{"ecosystem":"PyPI","name":"requests"}}]}`)
	add("notes.txt", "ignored")  // non-json skipped
	add("bad.json", `{not json`) // malformed skipped
	_ = zw.Close()

	recs, err := loadRecordsFromZip(buf.Bytes())
	if err != nil {
		t.Fatalf("loadRecordsFromZip: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d: %+v", len(recs), recs)
	}
	if db := NewDB(recs); db.Len() != 2 {
		t.Errorf("DB.Len() = %d, want 2", db.Len())
	}
}

// TestResolve_OfflineNoCacheIsEmpty proves the determinism-safe default: with no
// cache and no network fetch, Resolve yields an empty DB rather than erroring —
// so a --vuln-scan with no snapshot simply finds nothing.
func TestResolve_OfflineNoCacheIsEmpty(t *testing.T) {
	r, err := Resolve(ResolveConfig{Ecosystems: []string{"npm", "pypi"}, CacheDir: t.TempDir(), NoUpdate: true})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.DB.Len() != 0 {
		t.Errorf("offline + empty cache should yield an empty DB, got %d", r.DB.Len())
	}
	if !r.FromCache {
		t.Error("offline resolve should report FromCache=true")
	}
}

// TestReadAllProgress_ReportsCumulativeBytes proves the download status hook
// reports a monotonic byte count ending at the exact total, and returns the full
// data unchanged (so the snapshot hash is unaffected).
func TestReadAllProgress_ReportsCumulativeBytes(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 3<<20) // 3 MiB → reports near 1/2/3 MiB
	var reports []int64
	out, err := readAllProgress(bytes.NewReader(data), func(n int64) { reports = append(reports, n) })
	if err != nil {
		t.Fatalf("readAllProgress: %v", err)
	}
	if len(out) != len(data) {
		t.Fatalf("returned %d bytes, want %d", len(out), len(data))
	}
	if len(reports) == 0 {
		t.Fatal("expected at least one progress report")
	}
	for i := 1; i < len(reports); i++ {
		if reports[i] < reports[i-1] {
			t.Errorf("reports not monotonic: %v", reports)
		}
	}
	if last := reports[len(reports)-1]; last != int64(len(data)) {
		t.Errorf("final report %d != total %d", last, len(data))
	}
}

// TestReadAllProgress_NilCallback proves a nil hook is safe (the vulndb-pull /
// no-UI path).
func TestReadAllProgress_NilCallback(t *testing.T) {
	out, err := readAllProgress(bytes.NewReader([]byte("hello")), nil)
	if err != nil || string(out) != "hello" {
		t.Fatalf("readAllProgress(nil) = %q, %v", out, err)
	}
}

// TestResolve_OnProgressReportsEachEcosystem proves Resolve fires the UI hook
// with a start event and a finished event (carrying the cache source + record
// count) for each ecosystem it resolves.
func TestResolve_OnProgressReportsEachEcosystem(t *testing.T) {
	dir := t.TempDir()
	// Stage a cached PyPI snapshot so the offline path loads a real record.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("PYSEC-1.json")
	_, _ = w.Write([]byte(`{"id":"PYSEC-1","affected":[{"package":{"ecosystem":"PyPI","name":"requests"}}]}`))
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "PyPI.zip"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	var starts, finishes int
	var last ResolveProgress
	if _, err := Resolve(ResolveConfig{
		Ecosystems: []string{"pypi"},
		CacheDir:   dir,
		NoUpdate:   true, // offline: load from the staged cache, no fetch
		OnProgress: func(p ResolveProgress) {
			switch {
			case p.Finished:
				finishes++
				last = p
			case p.BytesRead == 0:
				starts++
			}
		},
	}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if starts != 1 || finishes != 1 {
		t.Fatalf("want 1 start + 1 finish event, got %d/%d", starts, finishes)
	}
	if !last.FromCache || last.Records != 1 || last.OSVEcosystem != "PyPI" || last.Index != 1 || last.Total != 1 {
		t.Errorf("unexpected finish event: %+v", last)
	}
}
