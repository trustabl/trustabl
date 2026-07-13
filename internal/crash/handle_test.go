package crash

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func TestHandleAlwaysWritesFileAndDoesNotPromptInCI(t *testing.T) {
	// Redirect HOME so DefaultConfigDir() writes into a temp dir.
	home := t.TempDir()
	t.Setenv("HOME", home)
	// CI=1 ensures isInteractive() returns false — no stdin read, no prompt.
	t.Setenv("CI", "1")
	// Clear any env override so the config file is the mode source.
	t.Setenv("TRUSTABL_TELEMETRY", "")

	sink := telemetry.NewRecordingSink()
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.json")
	if err := telemetry.SaveConfig(path, telemetry.Config{Mode: "full", AnonymousID: "id"}); err != nil {
		t.Fatal(err)
	}
	tel := telemetry.NewWithSink(sink, "1.0.0", path)

	Handle("boom", []byte("goroutine 1 [running]:\nmain.f()\n\t/x/main.go:1 +0x1\n"), Meta{Version: "1.0.0"}, tel)

	// A crash file must have been created under $HOME/.config/trustabl/.
	pattern := filepath.Join(home, ".config", "trustabl", "crash-*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected crash-*.log under %s/.config/trustabl/, got none", home)
	}

	// Because CI=1, isInteractive() is false, so act() returns immediately
	// without prompting or transmitting — zero crash.reported events.
	if len(sink.Events) != 0 {
		t.Fatalf("expected 0 events (CI, non-interactive), got %d: %#v", len(sink.Events), sink.Events)
	}
}

func newTestClient(t *testing.T, mode string) *telemetry.Client {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.json")
	if err := telemetry.SaveConfig(path, telemetry.Config{Mode: mode, AnonymousID: "id"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRUSTABL_TELEMETRY", "")
	return telemetry.NewWithSink(telemetry.NewRecordingSink(), "1.0.0", path)
}

func TestActSendTracksCrash(t *testing.T) {
	sink := telemetry.NewRecordingSink()
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "full", AnonymousID: "id"})
	t.Setenv("TRUSTABL_TELEMETRY", "")
	tel := telemetry.NewWithSink(sink, "1.0.0", path)

	var out strings.Builder
	rep := Report{PanicValue: "boom"}
	opened := ""
	act(&out, strings.NewReader("1\n"), true, rep, "/tmp/crash.log", tel,
		func(u string) error { opened = u; return nil })

	if len(sink.Events) != 1 || sink.Events[0].Name != "crash.reported" {
		t.Fatalf("expected crash.reported, got %#v", sink.Events)
	}
	if opened != "" {
		t.Fatalf("Send must not open a browser, opened %q", opened)
	}
}

func TestActSendWorksWhenTelemetryDisabled(t *testing.T) {
	// Crash reporting is decoupled from telemetry: choosing Send transmits even
	// when telemetry is disabled (the per-crash prompt is its own consent).
	sink := telemetry.NewRecordingSink()
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "disabled", AnonymousID: "id"})
	t.Setenv("TRUSTABL_TELEMETRY", "")
	tel := telemetry.NewWithSink(sink, "1.0.0", path)

	var out strings.Builder
	act(&out, strings.NewReader("1\n"), true, Report{PanicValue: "boom"}, "/tmp/c.log", tel,
		func(string) error { return nil })

	if len(sink.Events) != 1 || sink.Events[0].Name != "crash.reported" {
		t.Fatalf("Send must transmit even when telemetry disabled, got %#v", sink.Events)
	}
}

func TestActGitHubOpensURL(t *testing.T) {
	tel := newTestClient(t, "full")
	var out strings.Builder
	opened := ""
	act(&out, strings.NewReader("2\n"), true, Report{PanicValue: "boom"}, "/tmp/c.log", tel,
		func(u string) error { opened = u; return nil })
	if !strings.Contains(opened, "github.com/trustabl/trustabl/issues/new") {
		t.Fatalf("GitHub choice did not open issue URL: %q", opened)
	}
}

func TestActNonInteractiveDoesNothing(t *testing.T) {
	sink := telemetry.NewRecordingSink()
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "full", AnonymousID: "id"})
	t.Setenv("TRUSTABL_TELEMETRY", "")
	tel := telemetry.NewWithSink(sink, "1.0.0", path)

	var out strings.Builder
	act(&out, strings.NewReader("1\n"), false /*interactive*/, Report{}, "/tmp/c.log", tel,
		func(string) error { return nil })

	if len(sink.Events) != 0 {
		t.Fatalf("non-interactive must not transmit, got %#v", sink.Events)
	}
	if out.Len() != 0 {
		t.Fatalf("non-interactive must not prompt, wrote %q", out.String())
	}
}
