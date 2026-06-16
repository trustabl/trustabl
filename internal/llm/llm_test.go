package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/llm"
)

// llmEnvVars are the environment variables Load consults before the on-disk
// config. Tests must neutralize all of them so a key or model in the
// developer's shell never bleeds into a test (and never leaks into output).
var llmEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"GOOGLE_API_KEY",
	"TRUSTABL_LLM_MODEL",
}

// setConfigDir overrides the config directory for the duration of the test and
// clears the env-var key/model path so tests are isolated from a caller's
// shell. Clearing uses t.Setenv (which auto-restores on cleanup) rather than
// os.Unsetenv, so the developer's real environment is left intact.
func setConfigDir(t *testing.T, dir string) {
	t.Helper()
	old := llm.ConfigDir
	llm.ConfigDir = dir
	t.Cleanup(func() { llm.ConfigDir = old })
	for _, k := range llmEnvVars {
		t.Setenv(k, "")
	}
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
			name:     "openai valid key",
			provider: "openai",
			key:      "sk-proj-AAAAAAAAAAAAAAAAAAAAAA",
			wantErr:  false,
		},
		{
			name:     "openai wrong format key",
			provider: "openai",
			key:      "not-an-openai-key",
			wantErr:  true,
		},
		{
			name:     "openai empty key",
			provider: "openai",
			key:      "",
			wantErr:  true,
		},
		{
			name:     "google valid key",
			provider: "google",
			key:      "AIzaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			wantErr:  false,
		},
		{
			name:     "google wrong format key",
			provider: "google",
			key:      "not-a-google-key",
			wantErr:  true,
		},
		{
			name:     "google empty key",
			provider: "google",
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

func TestDefaultModels(t *testing.T) {
	tests := []struct {
		provider  string
		wantModel string
	}{
		{"anthropic", "claude-haiku-4-5"},
		{"openai", "gpt-4.1-nano"},
		{"google", "gemini-2.5-flash-lite"},
		{"localllm", ""},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			// Use a fresh config with no existing entry to trigger auto-create.
			cfg := &llm.Config{
				Active:    "other",
				Providers: map[string]llm.Provider{},
			}
			cfg.SetActive(tt.provider)
			p := cfg.Providers[tt.provider]
			if p.Model != tt.wantModel {
				t.Errorf("SetActive(%q) default model = %q, want %q", tt.provider, p.Model, tt.wantModel)
			}
		})
	}
}

func TestSetActive_NewProvider(t *testing.T) {
	setConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	cfg.SetActive("openai")

	if cfg.Active != "openai" {
		t.Errorf("Active = %q, want openai", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Model != "gpt-4.1-nano" {
		t.Errorf("Model = %q, want gpt-4.1-nano", p.Model)
	}
	if p.Key != "" {
		t.Errorf("Key = %q, want empty", p.Key)
	}
}

func TestSetActive_ExistingProvider(t *testing.T) {
	setConfigDir(t, t.TempDir())

	cfg, _ := llm.Load()
	// Set a key on the default anthropic provider.
	cfg.SetKey("sk-ant-api03-testkey12345678901234")
	// Switch to openai and back — the anthropic key must survive.
	cfg.SetActive("openai")
	cfg.SetActive("anthropic")

	if cfg.Active != "anthropic" {
		t.Errorf("Active = %q, want anthropic", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Key != "sk-ant-api03-testkey12345678901234" {
		t.Errorf("Key = %q, want original key preserved", p.Key)
	}
}

func TestLoad_NonDefaultActiveProvider(t *testing.T) {
	dir := t.TempDir()
	setConfigDir(t, dir)

	// Save a config with openai as active provider (no openai entry yet).
	cfg, _ := llm.Load()
	cfg.SetActive("openai")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load it back — the openai entry must have the openai default model,
	// not the anthropic default.
	got, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	p := got.ActiveProvider()
	if p.Model != "gpt-4.1-nano" {
		t.Errorf("openai default model = %q, want gpt-4.1-nano", p.Model)
	}
}

func TestIsKnownProvider(t *testing.T) {
	for _, p := range []string{"anthropic", "openai", "google"} {
		if !llm.IsKnownProvider(p) {
			t.Errorf("IsKnownProvider(%q) = false, want true", p)
		}
	}
	if llm.IsKnownProvider("anthropc") {
		t.Error("IsKnownProvider(typo) = true, want false")
	}
	if llm.IsKnownProvider("") {
		t.Error(`IsKnownProvider("") = true, want false`)
	}
}

func TestKnownProviders_SortedAndComplete(t *testing.T) {
	got := llm.KnownProviders()
	want := []string{"anthropic", "google", "openai"}
	if len(got) != len(want) {
		t.Fatalf("KnownProviders() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KnownProviders()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoad_EnvVarKey_Anthropic(t *testing.T) {
	setConfigDir(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-AAAAAAAAAAAAAAAAAAAA")

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Active != "anthropic" {
		t.Errorf("Active = %q, want anthropic", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Key != "sk-ant-api03-AAAAAAAAAAAAAAAAAAAA" {
		t.Errorf("Key = %q, want sk-ant-api03-AAAAAAAAAAAAAAAAAAAA", p.Key)
	}
	if p.Model != "claude-haiku-4-5" {
		t.Errorf("Model = %q, want claude-haiku-4-5", p.Model)
	}
}

func TestLoad_EnvVarKey_OpenAI(t *testing.T) {
	setConfigDir(t, t.TempDir())
	t.Setenv("OPENAI_API_KEY", "sk-proj-AAAAAAAAAAAAAAAAAAAAAA")

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Active != "openai" {
		t.Errorf("Active = %q, want openai", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Key != "sk-proj-AAAAAAAAAAAAAAAAAAAAAA" {
		t.Errorf("Key = %q, want sk-proj-AAAAAAAAAAAAAAAAAAAAAA", p.Key)
	}
	if p.Model != "gpt-4.1-nano" {
		t.Errorf("Model = %q, want gpt-4.1-nano", p.Model)
	}
}

func TestLoad_EnvVarKey_Google(t *testing.T) {
	setConfigDir(t, t.TempDir())
	t.Setenv("GOOGLE_API_KEY", "AIzaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Active != "google" {
		t.Errorf("Active = %q, want google", cfg.Active)
	}
	p := cfg.ActiveProvider()
	if p.Key != "AIzaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Errorf("Key = %q, want AIzaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", p.Key)
	}
	if p.Model != "gemini-2.5-flash-lite" {
		t.Errorf("Model = %q, want gemini-2.5-flash-lite", p.Model)
	}
}

func TestLoad_EnvVarKey_AnthropicTakesPriorityOverOpenAI(t *testing.T) {
	setConfigDir(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-AAAAAAAAAAAAAAAAAAAA")
	t.Setenv("OPENAI_API_KEY", "sk-proj-AAAAAAAAAAAAAAAAAAAAAA")

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Active != "anthropic" {
		t.Errorf("Active = %q, want anthropic (Anthropic should win when both set)", cfg.Active)
	}
}
