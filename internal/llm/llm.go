// Package llm manages LLM provider configuration for Trustabl.
// Configuration is stored in ~/.config/trustabl/keys.json (mode 0600).
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultProvider = "anthropic"
	defaultModel    = "claude-haiku-4-5"
)

// ConfigDir overrides os.UserConfigDir() when non-empty. Intended for tests only.
var ConfigDir string

// Config holds the full LLM configuration.
type Config struct {
	Active    string              `json:"active"`
	Providers map[string]Provider `json:"providers"`
}

// Provider holds the model and API key for a single LLM provider.
type Provider struct {
	Model string `json:"model"`
	Key   string `json:"key"`
}

// ActiveProvider returns the Provider entry for the active provider name.
func (c *Config) ActiveProvider() Provider {
	return c.Providers[c.Active]
}

// SetKey sets the API key for the active provider.
func (c *Config) SetKey(key string) {
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	p := c.Providers[c.Active]
	p.Key = key
	c.Providers[c.Active] = p
}

// ClearKey removes the API key for the active provider.
func (c *Config) ClearKey() {
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	p := c.Providers[c.Active]
	p.Key = ""
	c.Providers[c.Active] = p
}

// SetModel sets the model for the active provider.
func (c *Config) SetModel(model string) {
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	p := c.Providers[c.Active]
	p.Model = model
	c.Providers[c.Active] = p
}

// Load reads configuration from disk. Returns defaults when the file does not exist.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("llm: read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("llm: parse config: %w", err)
	}
	if c.Active == "" {
		c.Active = defaultProvider
	}
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	if _, ok := c.Providers[c.Active]; !ok {
		c.Providers[c.Active] = Provider{Model: defaultModel}
	}
	if p, ok := c.Providers[c.Active]; ok && p.Model == "" {
		p.Model = defaultModel
		c.Providers[c.Active] = p
	}
	return &c, nil
}

// Save writes configuration to disk atomically with mode 0600.
func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("llm: create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("llm: marshal config: %w", err)
	}
	tmpf, err := os.CreateTemp(filepath.Dir(path), ".keys-*.json.tmp")
	if err != nil {
		return fmt.Errorf("llm: create temp config: %w", err)
	}
	tmp := tmpf.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op after successful rename
	if _, err := tmpf.Write(data); err != nil {
		_ = tmpf.Close()
		return fmt.Errorf("llm: write config: %w", err)
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("llm: close temp config: %w", err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return fmt.Errorf("llm: chmod config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("llm: rename config: %w", err)
	}
	return nil
}

// Exists reports whether the configuration file exists on disk.
// Returns false only when the file is confirmed absent (ErrNotExist).
// Any other error (permissions, I/O) returns true — safer default than
// treating an unreadable config as "not configured".
func Exists() bool {
	path, err := configPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

func configPath() (string, error) {
	dir := ConfigDir
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("llm: config dir: %w", err)
		}
	}
	return filepath.Join(dir, "trustabl", "keys.json"), nil
}

func defaults() *Config {
	return &Config{
		Active: defaultProvider,
		Providers: map[string]Provider{
			defaultProvider: {Model: defaultModel},
		},
	}
}
