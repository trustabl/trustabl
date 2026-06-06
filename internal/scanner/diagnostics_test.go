package scanner_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/scanner"
)

// corpusTarget returns an absolute path to a corpus repo, resolved relative to
// this test file so it works regardless of the working directory.
func corpusTarget(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", name)
}

// A verbose logger attached to a scan emits a stderr diagnostic line for every
// pipeline phase. This guards the "proper functionality" of -v end to end:
// every scan, regardless of corpus content, narrates recon/inventory/policy/
// analysis. The lines are stderr-only and never alter the result.
func TestRun_VerboseEmitsPhaseDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	cfg := scanner.Config{
		Target:  corpusTarget("google-adk-demo"),
		RulesFS: rulesFixture(t),
		Log:     logx.New(&buf, logx.LevelVerbose, false),
	}
	if _, err := scanner.Run(cfg); err != nil {
		t.Fatalf("scan: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"recon:", "inventory:", "policy:", "analysis:"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing a %q line:\n%s", want, out)
		}
	}
	// Timing is debug-only; verbose must not emit it.
	if strings.Contains(out, "took ") {
		t.Errorf("verbose output unexpectedly contains debug timing:\n%s", out)
	}
	// Diagnostics carry the verbose tag and no debug tag.
	if !strings.Contains(out, "[verbose]") || strings.Contains(out, "[debug]") {
		t.Errorf("verbose output has wrong tags:\n%s", out)
	}
}

// A normal-level logger and a nil logger both produce no diagnostics — the
// scan stays silent on stderr, preserving the byte-stable contract on the
// normal path.
func TestRun_NormalAndNilLoggerSilent(t *testing.T) {
	target := corpusTarget("google-adk-demo")

	var buf bytes.Buffer
	cfg := scanner.Config{
		Target:  target,
		RulesFS: rulesFixture(t),
		Log:     logx.New(&buf, logx.LevelNormal, false),
	}
	if _, err := scanner.Run(cfg); err != nil {
		t.Fatalf("scan (normal level): %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("normal-level logger emitted output:\n%s", buf.String())
	}

	// A nil logger must not panic and must still produce a result.
	if _, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t), Log: nil}); err != nil {
		t.Fatalf("scan (nil log): %v", err)
	}
}

// Debug adds per-phase timing on top of the verbose lines.
func TestRun_DebugEmitsTiming(t *testing.T) {
	var buf bytes.Buffer
	cfg := scanner.Config{
		Target:  corpusTarget("google-adk-demo"),
		RulesFS: rulesFixture(t),
		Log:     logx.New(&buf, logx.LevelDebug, false),
	}
	if _, err := scanner.Run(cfg); err != nil {
		t.Fatalf("scan: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[debug]") || !strings.Contains(out, "took ") {
		t.Errorf("debug output missing timing lines:\n%s", out)
	}
}
