// Package ingestion implements the Importer + Normalizer of architecture §2.
//
// Importer resolves a user-supplied target (local path OR GitHub URL) to a
// readable directory on disk. For remote repos it shallow-clones to a temp dir
// and returns a Cleanup that the caller MUST defer.
package ingestion

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

// Source is a resolved input ready to hand to the Normalizer.
type Source struct {
	RootPath  string // absolute path on disk
	IsRemote  bool
	RemoteURL string // empty if local
	Cleanup   func() // no-op for local; rm -rf temp dir for remote
}

// Resolve takes the user's --target argument and returns a Source.
//
// prog, when non-nil, receives remote-clone progress (an accurate
// receiving-objects bar). It is ignored for local targets. Pass nil for none.
//
// Discipline: callers MUST `defer src.Cleanup()` even on local paths. The
// no-op cleanup avoids a branching defer at every call site.
func Resolve(target string, prog CloneProgress) (*Source, error) {
	if target == "" {
		return nil, errors.New("empty target")
	}

	if IsRemote(target) {
		return cloneRemote(target, prog)
	}
	return openLocal(target)
}

// IsRemote returns true if target appears to be a remote URL (so resolving it
// will clone). Conservative: anything we can't parse as a URL with a host is
// treated as a local path. Exported so the scanner can report a clone phase
// only for remote targets.
func IsRemote(target string) bool {
	// Common shorthand: git@github.com:owner/repo.git
	if strings.HasPrefix(target, "git@") {
		return true
	}
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

func openLocal(target string) (*Source, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	return &Source{
		RootPath: abs,
		IsRemote: false,
		Cleanup:  func() {},
	}, nil
}

func cloneRemote(remoteURL string, prog CloneProgress) (*Source, error) {
	tmp, err := os.MkdirTemp("", "trustabl-clone-*")
	if err != nil {
		return nil, fmt.Errorf("mktmp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	// Primary path: a plumbing-level shallow fetch that reports an accurate
	// receiving-objects bar (go-git's high-level PlainClone can't — it only
	// surfaces the server-side counting/compressing sideband, then goes silent
	// through the actual download). We only need the working tree, not history.
	if err := fetchTreeToDir(context.Background(), remoteURL, tmp, prog); err != nil {
		// Fall back to the proven PlainClone for anything the plumbing path can't
		// handle — notably private/SSH auth, which go-git's defaults cover. Reset
		// the temp dir first (the failed attempt may have written a partial tree).
		_ = os.RemoveAll(tmp)
		if mkErr := os.MkdirAll(tmp, 0o755); mkErr != nil {
			cleanup()
			return nil, fmt.Errorf("clone %s: reset temp: %w", remoteURL, mkErr)
		}
		if prog != nil {
			// The plumbing fetch may have advanced the receiving-objects bar before
			// failing; clear it so the fallback shows a live "cloning…" spinner
			// rather than a bar frozen mid-fill.
			prog.ResetPhase()
			prog.SetDetail("cloning…")
		}
		if _, e2 := git.PlainClone(tmp, false, &git.CloneOptions{
			URL: remoteURL, Depth: 1, Progress: nil,
		}); e2 != nil {
			cleanup()
			return nil, fmt.Errorf("clone %s: %w", remoteURL, e2)
		}
	}

	return &Source{
		RootPath:  tmp,
		IsRemote:  true,
		RemoteURL: remoteURL,
		Cleanup:   cleanup,
	}, nil
}
