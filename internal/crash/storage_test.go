package crash

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestWriteFileCreatesReadableReport(t *testing.T) {
	dir := t.TempDir()
	r := Report{PanicValue: "boom", Stack: []string{"main.f(...)"}, Version: "9.9.9"}
	ts := time.Date(2026, 7, 13, 14, 3, 22, 0, time.UTC)

	path, err := r.WriteFile(dir, ts)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !strings.HasSuffix(path, "crash-20260713-140322.log") {
		t.Fatalf("unexpected filename: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "boom") || !strings.Contains(string(data), "9.9.9") {
		t.Fatalf("report body missing content: %q", data)
	}
}
