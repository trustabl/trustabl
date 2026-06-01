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
	"time"

	"github.com/go-git/go-git/v5"
)

// cloneTimeout bounds a remote target clone. A hung or slow remote must not
// stall the scan forever; without a deadline the only escape is SIGINT.
const cloneTimeout = 120 * time.Second

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
		if err := validateRemoteScheme(target); err != nil {
			return nil, err
		}
		return cloneRemote(target, prog)
	}
	return openLocal(target)
}

// validateRemoteScheme rejects remote targets whose transport is unsafe. The
// scan target is user-supplied; a git:// URL is cleartext/unauthenticated and an
// ext:: URL is a remote-command-execution transport. Only http(s) and ssh
// (including the scp-like git@host:path shorthand) are permitted to reach the
// clone path. file:// never gets here — IsRemote treats a hostless URL as local.
func validateRemoteScheme(target string) error {
	if strings.HasPrefix(target, "git@") {
		return nil // scp-like SSH shorthand
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil // unparseable → IsRemote would not have routed it here
	}
	switch u.Scheme {
	case "http", "https", "ssh":
		return nil
	default:
		return fmt.Errorf("unsupported remote transport %q (allowed: https, ssh): %s", u.Scheme, target)
	}
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

	// Bound the whole clone (both the plumbing fetch and the PlainClone
	// fallback) so a hung remote cannot stall the scan indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()

	// Primary path: a plumbing-level shallow fetch that reports an accurate
	// receiving-objects bar (go-git's high-level PlainClone can't — it only
	// surfaces the server-side counting/compressing sideband, then goes silent
	// through the actual download). We only need the working tree, not history.
	if err := fetchTreeToDir(ctx, remoteURL, tmp, prog); err != nil {
		// Fall back to the proven PlainClone for anything the plumbing path can't
		// handle — notably private/SSH auth, which go-git's defaults cover. Reset
		// the temp dir first (the failed attempt may have written a partial tree).
		_ = os.RemoveAll(tmp)
		if mkErr := os.MkdirAll(tmp, 0o700); mkErr != nil {
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
		if _, e2 := git.PlainCloneContext(ctx, tmp, false, &git.CloneOptions{
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
