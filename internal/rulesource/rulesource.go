// Package rulesource resolves Trustabl's detection rules from an external git
// repository into a local cache, and hands the engine an fs.FS rooted at the
// chosen rule pack. It owns fetch, cache, and the schema-compatibility gate.
package rulesource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRepoURL is the canonical rules repository pulled at scan time. It is
// overridable via Config.RepoURL (--rules-repo / TRUSTABL_RULES_REPO).
const DefaultRepoURL = "https://github.com/trustabl/trustabl-rules"

// ErrNoRules means no rule pack could be made available — neither fetched nor
// found in cache. A scan in this state must fail (exit 2), never report clean.
var ErrNoRules = errors.New("no rules available: none cached and could not fetch")

// ErrNoCompatibleRules means a pack was available but its schema_version
// exceeds what this engine build supports.
var ErrNoCompatibleRules = errors.New("no schema-compatible rules available")

// fatalResolveError wraps a failure that must NOT degrade to cached rules: a
// local filesystem / install fault (disk full, permission denied, a failed
// rename, or a corrupt freshly-cloned repo). These are operator-environment
// problems, not "the remote is unreachable" — silently serving stale rules
// would mask a real failure. Remote-contact failures are deliberately left
// unwrapped so they stay fallback-eligible (the offline story).
type fatalResolveError struct{ err error }

func (e *fatalResolveError) Error() string { return e.err.Error() }
func (e *fatalResolveError) Unwrap() error { return e.err }

// cloneIntoFn is a seam so tests can simulate an install failure without
// forcing a real disk fault. Production always uses cloneInto.
var cloneIntoFn = cloneInto

// validateRepoURL rejects rules-repo URLs whose transport is unsafe. The repo
// URL is operator-controlled (--rules-repo / TRUSTABL_RULES_REPO), so an
// attacker-influenced value must not be able to turn a "remote fetch" into a
// local-filesystem read (file://) or a cleartext/unauthenticated transport
// (git://). A bare local path (empty scheme) is allowed — that is a legitimate
// offline/development rules source — as is the scp-like git@host:path form.
func validateRepoURL(raw string) error {
	if strings.HasPrefix(raw, "git@") {
		return nil // scp-like SSH shorthand
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return nil // local filesystem path, not a network transport
	}
	// A single-letter scheme is a Windows drive letter (C:\...), i.e. a local
	// path — url.Parse reports its drive as the "scheme". Treat it as local.
	if len(u.Scheme) == 1 {
		return nil
	}
	switch u.Scheme {
	case "http", "https", "ssh":
		return nil
	default:
		return fmt.Errorf("rules repo URL uses unsupported transport %q (allowed: https, ssh): %s", u.Scheme, raw)
	}
}

// Config controls one rule-source resolution.
type Config struct {
	RepoURL  string // rules repo; empty => DefaultRepoURL
	Ref      string // branch/tag to resolve; empty => remote default branch
	NoUpdate bool   // skip the network; use cache only
	CacheDir string // cache root; empty => os.UserCacheDir()/trustabl/rules
}

// Resolved is the outcome of a successful resolution.
type Resolved struct {
	FS        fs.FS  // rooted at the chosen pack directory
	SHA       string // resolved commit SHA — the rules "version"
	RepoURL   string // repo the pack came from
	FromCache bool   // true if the network was skipped/unreachable
}

// withDefaults returns cfg with empty fields filled in.
func withDefaults(cfg Config) (Config, error) {
	if cfg.RepoURL == "" {
		cfg.RepoURL = DefaultRepoURL
	}
	if cfg.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return cfg, fmt.Errorf("locate user cache dir: %w", err)
		}
		cfg.CacheDir = filepath.Join(base, "trustabl", "rules")
	}
	return cfg, nil
}

// usePack builds a Resolved for an already-cached SHA, gating on schema
// compatibility.
func usePack(cfg Config, sha string, fromCache bool, supported int) (Resolved, error) {
	fsys := os.DirFS(packDir(cfg.CacheDir, sha))
	if !compatible(fsys, supported) {
		return Resolved{}, ErrNoCompatibleRules
	}
	return Resolved{FS: fsys, SHA: sha, RepoURL: cfg.RepoURL, FromCache: fromCache}, nil
}

// fallbackToCache resolves the current cached pack, or ErrNoRules if none.
func fallbackToCache(cfg Config, supported int) (Resolved, error) {
	sha, ok := readCurrent(cfg.CacheDir)
	if !ok {
		return Resolved{}, ErrNoRules
	}
	return usePack(cfg, sha, true, supported)
}

// Resolve obtains a rule pack for a scan. With NoUpdate it uses the cache
// only. Otherwise it resolves the latest ref, clones it if new, and gates it.
// It falls back to the cached pack on a remote-contact failure (the offline
// story) or a schema incompatibility, but a local install fault — disk full,
// permission denied, a failed rename, or a corrupt clone (a fatalResolveError) —
// is propagated, never masked by stale cached rules. It returns ErrNoRules only
// when nothing is available at all.
func Resolve(cfg Config, supported int) (Resolved, error) {
	cfg, err := withDefaults(cfg)
	if err != nil {
		return Resolved{}, err
	}
	if err := validateRepoURL(cfg.RepoURL); err != nil {
		return Resolved{}, err
	}

	if cfg.NoUpdate {
		return fallbackToCache(cfg, supported)
	}

	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()

	sha, refName, err := resolveRef(ctx, cfg.RepoURL, cfg.Ref)
	if err != nil {
		return fallbackToCache(cfg, supported)
	}
	if !packExists(cfg.CacheDir, sha) {
		cloned, err := cloneIntoFn(ctx, cfg.RepoURL, refName, cfg.CacheDir)
		if err != nil {
			// A local install fault (disk full, permission, failed rename,
			// corrupt clone) must surface — falling back to stale cached rules
			// would mask it. Only remote-contact failures degrade to cache.
			var fe *fatalResolveError
			if errors.As(err, &fe) {
				return Resolved{}, fmt.Errorf("resolve rules: %w", err)
			}
			return fallbackToCache(cfg, supported)
		}
		sha = cloned // authoritative: the commit actually cloned (see cloneInto)
	}
	res, err := usePack(cfg, sha, false, supported)
	if err != nil {
		// Pack fetched but unusable (incompatible schema). Try the last
		// known-good cached pack before giving up.
		if fb, fbErr := fallbackToCache(cfg, supported); fbErr == nil {
			return fb, nil
		}
		return Resolved{}, err
	}
	if err := writeCurrent(cfg.CacheDir, sha); err != nil {
		return Resolved{}, fmt.Errorf("record current rules pointer: %w", err)
	}
	pruneCache(cfg.CacheDir, sha)
	return res, nil
}

// Pull is the explicit `trustabl rules pull` path: it always contacts the
// remote and returns an error if it cannot fetch — it does not fall back to
// cache, because the user asked for a fetch.
func Pull(cfg Config, supported int) (Resolved, error) {
	cfg, err := withDefaults(cfg)
	if err != nil {
		return Resolved{}, err
	}
	if err := validateRepoURL(cfg.RepoURL); err != nil {
		return Resolved{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	sha, refName, err := resolveRef(ctx, cfg.RepoURL, cfg.Ref)
	if err != nil {
		return Resolved{}, err
	}
	if !packExists(cfg.CacheDir, sha) {
		cloned, err := cloneInto(ctx, cfg.RepoURL, refName, cfg.CacheDir)
		if err != nil {
			return Resolved{}, err
		}
		sha = cloned // authoritative: the commit actually cloned (see cloneInto)
	}
	res, err := usePack(cfg, sha, false, supported)
	if err != nil {
		return Resolved{}, err
	}
	if err := writeCurrent(cfg.CacheDir, sha); err != nil {
		return Resolved{}, fmt.Errorf("record current rules pointer: %w", err)
	}
	pruneCache(cfg.CacheDir, sha)
	return res, nil
}
