package crash

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultConfigDir returns ~/.config/trustabl, matching the telemetry config
// location.
func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("crash: cannot find home dir: %w", err)
	}
	return filepath.Join(home, ".config", "trustabl"), nil
}

// WriteFile writes the report to dir/crash-<UTC-timestamp>.log (0600, dir 0700)
// and returns the path. ts should be UTC.
func (r Report) WriteFile(dir string, ts time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("crash: create dir: %w", err)
	}
	name := "crash-" + ts.Format("20060102-150405") + ".log"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(r.String()), 0o600); err != nil {
		return "", fmt.Errorf("crash: write file: %w", err)
	}
	return path, nil
}
