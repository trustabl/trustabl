// Package rulesource resolves Trustabl's detection rules from an external git
// repository into a local cache, and hands the engine an fs.FS rooted at the
// chosen rule pack. It owns fetch, cache, and the schema-compatibility gate.
package rulesource

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultRepoURL is the canonical rules repository. Phase 2 confirms the final
// org/URL before release; the value is overridable via Config.RepoURL
// (--rules-repo / TRUSTABL_RULES_REPO).
const DefaultRepoURL = "https://github.com/trustabl/trustabl-rules"

// ErrNoRules means no rule pack could be made available — neither fetched nor
// found in cache. A scan in this state must fail (exit 2), never report clean.
var ErrNoRules = errors.New("no rules available: none cached and could not fetch")

// ErrNoCompatibleRules means a pack was available but its schema_version
// exceeds what this engine build supports.
var ErrNoCompatibleRules = errors.New("no schema-compatible rules available")

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
// only. Otherwise it resolves the latest ref, clones it if new, and gates it;
// on any network/clone failure or incompatibility it falls back to the cached
// pack. It returns ErrNoRules only when nothing is available at all.
func Resolve(cfg Config, supported int) (Resolved, error) {
	cfg, err := withDefaults(cfg)
	if err != nil {
		return Resolved{}, err
	}

	if cfg.NoUpdate {
		return fallbackToCache(cfg, supported)
	}

	sha, refName, err := resolveRef(cfg.RepoURL, cfg.Ref)
	if err != nil {
		return fallbackToCache(cfg, supported)
	}
	if !packExists(cfg.CacheDir, sha) {
		cloned, err := cloneInto(cfg.RepoURL, refName, cfg.CacheDir)
		if err != nil {
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
	sha, refName, err := resolveRef(cfg.RepoURL, cfg.Ref)
	if err != nil {
		return Resolved{}, err
	}
	if !packExists(cfg.CacheDir, sha) {
		cloned, err := cloneInto(cfg.RepoURL, refName, cfg.CacheDir)
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
	return res, nil
}
