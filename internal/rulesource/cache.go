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

// pruneCache removes every entry in cacheDir except the kept SHA's pack
// directory and the `current` pointer file. This bounds the cache to a single
// pack (the active one) and also clears stale `.tmp-clone-*` directories left
// by interrupted clones. Best-effort: a pack a concurrent scan still holds open
// (e.g. a Windows file lock) simply fails to delete and is left for next time.
func pruneCache(cacheDir, keep string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep {
			continue // keep the active pack; the `current` file is not a dir
		}
		_ = os.RemoveAll(filepath.Join(cacheDir, e.Name()))
	}
}

// writeCurrent records sha as the cache's current pointer. The write is
// atomic — a temp file in the same directory followed by a rename — so an
// interrupted write can never leave a truncated pointer that readCurrent would
// misread.
func writeCurrent(cacheDir, sha string) error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
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
