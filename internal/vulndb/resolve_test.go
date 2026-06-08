package vulndb

import (
	"archive/zip"
	"bytes"
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
