// Package ingestion implements the Importer + Normalizer of architecture §2.
//
// Importer resolves a user-supplied target (local path OR GitHub URL) to a
// readable directory on disk. For remote repos it shallow-clones to a temp dir
// and returns a Cleanup that the caller MUST defer.
package ingestion

import (
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
// Discipline: callers MUST `defer src.Cleanup()` even on local paths. The
// no-op cleanup avoids a branching defer at every call site.
func Resolve(target string) (*Source, error) {
	if target == "" {
		return nil, errors.New("empty target")
	}

	if looksRemote(target) {
		return cloneRemote(target)
	}
	return openLocal(target)
}

// looksRemote returns true if target appears to be a remote URL. Conservative:
// anything we can't parse as a URL with a host is treated as a local path.
func looksRemote(target string) bool {
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

func cloneRemote(remoteURL string) (*Source, error) {
	tmp, err := os.MkdirTemp("", "karenctl-clone-*")
	if err != nil {
		return nil, fmt.Errorf("mktmp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	// Shallow clone — we only need source, not history.
	// Auth note: go-git will pick up SSH agent / GIT_ASKPASS for private repos.
	// BYOK for GitHub auth is out of scope for the skeleton; document it.
	_, err = git.PlainClone(tmp, false, &git.CloneOptions{
		URL:      remoteURL,
		Depth:    1,
		Progress: nil,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("clone %s: %w", remoteURL, err)
	}

	return &Source{
		RootPath:  tmp,
		IsRemote:  true,
		RemoteURL: remoteURL,
		Cleanup:   cleanup,
	}, nil
}
