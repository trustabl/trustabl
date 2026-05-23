package rulesource

import (
	"os"
	"path/filepath"
	"strings"
)

// packDir is the cache subdirectory for one resolved commit SHA.
func packDir(cacheDir, sha string) string {
	return filepath.Join(cacheDir, sha)
}

// packExists reports whether a pack for sha is already cloned in the cache.
func packExists(cacheDir, sha string) bool {
	info, err := os.Stat(packDir(cacheDir, sha))
	return err == nil && info.IsDir()
}

// currentFile names the cache's "current" pointer — a text file holding the
// SHA most recently resolved successfully. It is the fallback target when the
// network is unreachable.
func currentFile(cacheDir string) string {
	return filepath.Join(cacheDir, "current")
}

// readCurrent returns the SHA recorded in the current pointer, if any.
func readCurrent(cacheDir string) (sha string, ok bool) {
	b, err := os.ReadFile(currentFile(cacheDir))
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", false
	}
	return s, true
}

// writeCurrent records sha as the cache's current pointer. The write is
// atomic — a temp file in the same directory followed by a rename — so an
// interrupted write can never leave a truncated pointer that readCurrent would
// misread.
func writeCurrent(cacheDir, sha string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir, ".tmp-current-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds
	if _, err := tmp.WriteString(sha + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), currentFile(cacheDir))
}
