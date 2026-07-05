package telemetry

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the persisted telemetry preference.
type Config struct {
	Enabled     bool   `json:"enabled"`
	AnonymousID string `json:"anonymous_id"`
}

// DefaultConfigPath returns ~/.config/trustabl/telemetry.json.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("telemetry: cannot find home dir: %w", err)
	}
	return filepath.Join(home, ".config", "trustabl", "telemetry.json"), nil
}

// LoadConfig reads the config at path. If the file does not exist, it returns
// a default config (Enabled: true, fresh UUID) and existed=false.
// A corrupt file returns an error.
func LoadConfig(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			id, genErr := newUUID()
			if genErr != nil {
				return Config{}, false, genErr
			}
			return Config{Enabled: true, AnonymousID: id}, false, nil
		}
		return Config{}, false, fmt.Errorf("telemetry: read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("telemetry: parse config: %w", err)
	}
	return cfg, true, nil
}

// SaveConfig writes cfg to path, creating directories as needed.
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("telemetry: create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// newUUID returns a random UUID v4.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
