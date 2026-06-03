package rulesource

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pruneGraceWindow is how recently a non-kept cache entry must have been
// modified to be spared from pruning. It protects packs (and in-progress
// temp-clone dirs) that a concurrent scan may still be materializing or
// reading: the resolved pack's files are read lazily via os.DirFS *after*
// Resolve returns, so deleting a freshly created pack out from under another
// in-flight scan fails that scan (notably on Windows, where a file deleted
// before it is read errors rather than surviving via an open handle). It is
// far longer than any scan takes, and because the same SHA is reused via
// packExists, the cache still converges to a single pack between runs.
const pruneGraceWindow = 30 * time.Minute

// packDir is the cache subdirectory for one resolved commit SHA.
func packDir(cacheDir, sha string) string {
	return filepath.Join(cacheDir, sha)
}

// completeMarker is the sentinel file written as the final step of installing a
// pack (see cloneInto). packExists requires it, and pruneCache deletes it FIRST
// (before RemoveAll), so a pack left half-deleted by an interrupted prune has no
// marker and is treated as absent — re-cloned — rather than trusted as a
// silently-thinned ruleset. Not a .yaml, so the loader's walk ignores it.
const completeMarker = ".complete"

// markPackComplete writes the completeness sentinel into packDirPath. Called on
// the temp clone dir just before the atomic rename, so the installed pack
// carries the marker.
func markPackComplete(packDirPath string) error {
	f, err := os.Create(filepath.Join(packDirPath, completeMarker))
	if err != nil {
		return err
	}
	return f.Close()
}

// packExists reports whether a *complete* pack for sha is cached: the directory
// exists AND carries the completeness marker. A markerless directory is a
// partial pack (e.g. an interrupted prior prune) and is reported absent so the
// caller re-clones rather than loading a thinned ruleset.
func packExists(cacheDir, sha string) bool {
	dir := packDir(cacheDir, sha)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, completeMarker))
	return err == nil
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
		// Spare entries modified within the grace window — a concurrent scan may
		// still be reading or materializing them. Genuinely stale packs and
		// abandoned temp-clone dirs from prior runs have old mtimes and are
		// pruned. If the mtime can't be read, fall through to pruning (the prior
		// best-effort behavior).
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) < pruneGraceWindow {
			continue
		}
		// Delete the completeness sentinel FIRST: if the subsequent RemoveAll is
		// interrupted, the remnant has no marker and packExists distrusts it
		// (re-clone) instead of loading a half-deleted pack.
		_ = os.Remove(filepath.Join(cacheDir, e.Name(), completeMarker))
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
