package telemetry_test

import (
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func TestNullSink_noop(t *testing.T) {
	s := telemetry.NewNullSink()
	s.Track("any.event", map[string]any{"key": "value"}) // must not panic
	s.Flush()                                             // must not panic
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
	if !cfg.Enabled {
		t.Error("want default Enabled=true")
	}
	if cfg.AnonymousID == "" {
		t.Error("want non-empty AnonymousID generated for new config")
	}
}

func TestSaveAndLoadConfig_roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	original := telemetry.Config{Enabled: false, AnonymousID: "test-uuid-123"}
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
	if loaded.Enabled != false {
		t.Error("want Enabled=false")
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
	// clear known CI vars so the test is hermetic
	vars := []string{"GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "JENKINS_URL", "CI"}
	for _, v := range vars {
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
	vars := []string{"GITHUB_REPOSITORY", "CI_PROJECT_PATH", "CIRCLE_PROJECT_REPONAME"}
	for _, v := range vars {
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

func TestClient_enabledByEnvVar(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "1")
	path := filepath.Join(t.TempDir(), "telemetry.json")
	// save a config with enabled=false to confirm env var wins
	_ = telemetry.SaveConfig(path, telemetry.Config{Enabled: false, AnonymousID: "x"})
	c := telemetry.New("", "0.0.0", path, nil)
	if !c.IsEnabled() {
		t.Error("want enabled when TRUSTABL_TELEMETRY=1, even if config says false")
	}
}

func TestClient_disabledByConfig(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "") // clear env var
	path := filepath.Join(t.TempDir(), "telemetry.json")
	_ = telemetry.SaveConfig(path, telemetry.Config{Enabled: false, AnonymousID: "x"})
	c := telemetry.New("", "0.0.0", path, nil)
	if c.IsEnabled() {
		t.Error("want disabled when config has enabled=false")
	}
}

func TestClient_defaultEnabled(t *testing.T) {
	t.Setenv("TRUSTABL_TELEMETRY", "")
	path := filepath.Join(t.TempDir(), "telemetry.json") // does not exist
	c := telemetry.New("", "0.0.0", path, nil)
	if !c.IsEnabled() {
		t.Error("want enabled by default when no env var and no config file")
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
	_ = telemetry.SaveConfig(path, telemetry.Config{Enabled: true, AnonymousID: "x"})
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
