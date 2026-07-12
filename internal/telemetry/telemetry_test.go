package telemetry_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func TestNullSink_noop(t *testing.T) {
	s := telemetry.NewNullSink()
	s.Track("any.event", map[string]any{"key": "value"})
	s.Flush()
}

func TestRecordingSink_capturesEvents(t *testing.T) {
	s := telemetry.NewRecordingSink()
	s.Track("scan.started", map[string]any{"os": "darwin"})
	s.Track("scan.completed", map[string]any{"exit_code": 0})

	if len(s.Events) != 2 {
		t.Fatalf("want 2 events, got %d", len(s.Events))
	}
	if s.Events[0].Name != "scan.started" {
		t.Errorf("want scan.started, got %s", s.Events[0].Name)
	}
	if s.Events[1].Props["exit_code"] != 0 {
		t.Errorf("want exit_code=0, got %v", s.Events[1].Props["exit_code"])
	}
}

func TestLoadConfig_missingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	cfg, existed, err := telemetry.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Error("want existed=false for missing file")
	}
	if cfg.Mode != "" {
		t.Errorf("want Mode empty string for unset config, got %q", cfg.Mode)
	}
	if cfg.AnonymousID == "" {
		t.Error("want non-empty AnonymousID generated for new config")
	}
}

func TestSaveAndLoadConfig_roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	original := telemetry.Config{Mode: "disabled", AnonymousID: "test-uuid-123"}
	if err := telemetry.SaveConfig(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, existed, err := telemetry.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Error("want existed=true after save")
	}
	if loaded.Mode != "disabled" {
		t.Errorf("want Mode=disabled, got %s", loaded.Mode)
	}
	if loaded.AnonymousID != "test-uuid-123" {
		t.Errorf("want AnonymousID=test-uuid-123, got %s", loaded.AnonymousID)
	}
}

func TestDetectCIProvider_githubActions(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	if got := telemetry.DetectCIProvider(); got != "github_actions" {
		t.Errorf("want github_actions, got %s", got)
	}
}

func TestDetectCIProvider_notCI(t *testing.T) {
	for _, v := range []string{"GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "JENKINS_URL", "CI"} {
		t.Setenv(v, "")
	}
	if got := telemetry.DetectCIProvider(); got != "" {
		t.Errorf("want empty string for non-CI, got %s", got)
	}
}

func TestRepoIDHash_consistent(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	h1 := telemetry.RepoIDHash()
	h2 := telemetry.RepoIDHash()
	if h1 == "" {
		t.Fatal("want non-empty hash")
	}
	if h1 != h2 {
		t.Errorf("want stable hash, got %s and %s", h1, h2)
	}
}

func TestRepoIDHash_emptyWhenNoCI(t *testing.T) {
	for _, v := range []string{"GITHUB_REPOSITORY", "CI_PROJECT_PATH", "CIRCLE_PROJECT_REPONAME"} {
		t.Setenv(v, "")
	}
	if got := telemetry.RepoIDHash(); got != "" {
		t.Errorf("want empty hash when no CI repo env var, got %s", got)
	}
}

func TestClient_disabledByEnvVar(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "0")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled when TRUSTABL_TELEMETRY=0")
	}
}

func TestClient_disabledByEnvVarWord(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "disabled")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled when TRUSTABL_TELEMETRY=disabled")
	}
}

func TestClient_enabledByEnvVar(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "1")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "disabled", AnonymousID: "x"})
	c := telemetry.New("", "0.0.0", path, nil)
	if !c.IsEnabled() {
		t.Error("want enabled when TRUSTABL_TELEMETRY=1 even if config says disabled")
	}
}

func TestClient_minimalByEnvVar(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "minimal")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	c := telemetry.New("", "0.0.0", path, nil)
	if c.Mode() != "minimal" {
		t.Errorf("want mode=minimal, got %s", c.Mode())
	}
}

func TestClient_disabledByConfig(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "disabled", AnonymousID: "x"})
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled when config has mode=disabled")
	}
}

func TestClient_defaultDisabled(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json") // does not exist
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled by default when no env var and no config file")
	}
}

func TestClient_CI_defaultDisabled(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled in CI when TRUSTABL_TELEMETRY not set")
	}
}

func TestClient_isNewInstall_trueWhenNoConfig(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	c := telemetry.New("", "0.0.0", path, nil)
	if !c.IsNewInstall() {
		t.Error("want IsNewInstall=true when config did not exist")
	}
}

func TestClient_isNewInstall_falseWhenConfigExists(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Mode: "full", AnonymousID: "x"})
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsNewInstall() {
		t.Error("want IsNewInstall=false when config already existed")
	}
}

func TestClient_trackRoutesToSink(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "1")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	rec := telemetry.NewRecordingSink()
	c := telemetry.NewWithSink(rec, "0.0.0", path)
	c.Track("scan.started", map[string]any{"format": "human"})
	if len(rec.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(rec.Events))
	}
	if rec.Events[0].Name != "scan.started" {
		t.Errorf("want scan.started, got %s", rec.Events[0].Name)
	}
}

func TestClient_trackDroppedWhenDisabled(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "0")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	rec := telemetry.NewRecordingSink()
	c := telemetry.NewWithSink(rec, "0.0.0", path)
	c.Track("scan.started", map[string]any{})
	if len(rec.Events) != 0 {
		t.Errorf("want 0 events when disabled, got %d", len(rec.Events))
	}
}

func TestClient_ciEphemeralIDNotPersisted(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	telemetry.New("", "0.0.0", path, nil)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("config file must not be written in CI")
	}
}
