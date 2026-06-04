package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/llm"
)

// setConfigDir overrides the config directory for the duration of the test.
func setConfigDir(t *testing.T, dir string) {
	t.Helper()
	old := llm.ConfigDir
	llm.ConfigDir = dir
	t.Cleanup(func() { llm.ConfigDir = old })
}

func TestLoad_Defaults(t *testing.T) {
	setConfigDir(t, t.TempDir())

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Active != "anthropic" {
		t.Errorf("Active = %q, want anthropic", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Model != "claude-haiku-4-5" {
		t.Errorf("Model = %q, want claude-haiku-4-5", p.Model)
	}
	if p.Key != "" {
		t.Errorf("Key = %q, want empty", p.Key)
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	setConfigDir(t, t.TempDir())

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.SetKey("sk-ant-api03-testkey12345678901234")
	cfg.SetModel("claude-opus-4-7")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}
	if got.Active != "anthropic" {
		t.Errorf("Active = %q, want anthropic", got.Active)
	}
	p := got.ActiveProvider()
	if p.Key != "sk-ant-api03-testkey12345678901234" {
		t.Errorf("Key = %q, want sk-ant-api03-testkey12345678901234", p.Key)
	}
	if p.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want claude-opus-4-7", p.Model)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	setConfigDir(t, dir)

	cfg, _ := llm.Load()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	path := filepath.Join(dir, "trustabl", "keys.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permission = %04o, want 0600", perm)
	}
}

func TestSave_Atomic_NoTmpFile(t *testing.T) {
	dir := t.TempDir()
	setConfigDir(t, dir)

	cfg, _ := llm.Load()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "trustabl", ".keys-*.json.tmp"))
	if err != nil {
		t.Fatalf("Glob error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("tmp files leaked after Save(): %v", matches)
	}
}

func TestExists(t *testing.T) {
	setConfigDir(t, t.TempDir())

	if llm.Exists() {
		t.Error("Exists() = true before any Save, want false")
	}
	cfg, _ := llm.Load()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if !llm.Exists() {
		t.Error("Exists() = false after Save, want true")
	}
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		key      string
		wantErr  bool
	}{
		{
			name:     "valid anthropic key",
			provider: "anthropic",
			key:      "sk-ant-api03-testkey12345678901234",
			wantErr:  false,
		},
		{
			name:     "empty key",
			provider: "anthropic",
			key:      "",
			wantErr:  true,
		},
		{
			name:     "wrong prefix",
			provider: "anthropic",
			key:      "sk-openai-abc12345678901234567890",
			wantErr:  true,
		},
		{
			name:     "too short after prefix",
			provider: "anthropic",
			key:      "sk-ant-short",
			wantErr:  true,
		},
		{
			name:     "unknown provider accepts any non-empty key",
			provider: "openai",
			key:      "sk-proj-anything",
			wantErr:  false,
		},
		{
			name:     "unknown provider rejects empty key",
			provider: "openai",
			key:      "",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := llm.ValidateKey(tt.provider, tt.key)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateKey(%q, %q) = nil, want error", tt.provider, tt.key)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateKey(%q, %q) = %v, want nil", tt.provider, tt.key, err)
			}
		})
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"sk-ant-api03-abc123xyz789", "****...z789"},
		{"12345", "****...2345"},
		{"abcd", "****"},
		{"abc", "****"},
		{"", "(not set)"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := llm.MaskKey(tt.key)
			if got != tt.want {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
