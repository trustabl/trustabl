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
