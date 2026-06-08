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

// ErrNoCompatibleRules means a pack was available but its manifest is missing,
// unparseable, or declares a non-positive schema version — a pack the engine
// cannot vouch for. A pack that merely targets a NEWER schema than this build
// is no longer rejected here: it is resolved and loaded leniently (the lenient
// loader skips rules this build cannot evaluate), so this error is now reserved
// for an unreadable/corrupt manifest.
var ErrNoCompatibleRules = errors.New("no usable rules manifest")

// ErrNoTrustKeys means a signed channel was requested (--channel) but this
// engine build embeds no rule-signing trust keys, so no statement can be
// verified. Distinct from ErrNoRules so the CLI can advise dropping --channel
// rather than refreshing the cache. Expected until signing keys are published
// (RUL-2) and baked into a build.
var ErrNoTrustKeys = errors.New("no rule-signing trust keys embedded in this build")

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
	CacheDir string // git-pack cache root; empty => os.UserCacheDir()/trustabl/rules

	// BundleCacheDir is the cache root for the signed-distribution cache
	// (releaseSource's installed bundles + per-channel anti-rollback state).
	// Empty => os.UserCacheDir()/trustabl/bundles — a SIBLING of CacheDir, never
	// under it, so no rules-cache pruner (this build's or an older one's that
	// predates the signed path) can ever delete the signed cache.
	BundleCacheDir string

	// Channel selects the signed-distribution path: when non-empty, resolution
	// goes through releaseSource — verify a signed channel statement, then fetch
	// the bundle it commits to by digest — instead of the default git clone. An
	// empty Channel keeps the historical gitSource behavior, which stays the
	// default until the signed-channel cutover (ENG-6). Wiring of the user-facing
	// --channel flag (and its report watermark) lands with ENG-5; until then this
	// is opt-in via Config only.
	Channel string
}

// Resolved is the outcome of a successful resolution.
type Resolved struct {
	FS        fs.FS  // rooted at the chosen pack directory
	SHA       string // resolved commit SHA, or signed-bundle digest — the rules "version"
	RepoURL   string // repo the pack came from
	FromCache bool   // true if the network was skipped/unreachable

	// Stale is set by releaseSource when an offline fallback serves a cached
	// bundle whose last-verified channel statement has already expired — the
	// bundle is still authentic but the channel pointer may have moved on. Drives
	// a louder "rules may be out of date" warning than FromCache alone (ENG-5).
	Stale bool

	// SchemaVersion is the pack manifest's declared schema_version. SchemaNewer
	// is true when it exceeds the engine's supported version: the pack targets a
	// newer rule grammar, so the lenient loader will skip any rules using
	// predicates this build lacks. The pack is still used — these fields drive a
	// user-facing "rules newer than this build" warning, not a refusal.
	SchemaVersion int
	SchemaNewer   bool
}

// Source resolves rule packs for a scan. Two implementations exist: the default
// gitSource (clone a ref of the rules repo) and releaseSource (verify a signed
// channel statement and fetch the bundle it points to). SourceFor picks one from
// a Config; callers normally use the package-level Resolve/Pull wrappers, which
// route through SourceFor.
type Source interface {
	// Resolve obtains a pack for a scan, falling back to the cache on a
	// remote-contact failure (the offline story) but never on a local fault.
	Resolve(cfg Config, supported int) (Resolved, error)
	// Pull is the explicit-fetch path (`trustabl rules pull`): it always contacts
	// the remote and never falls back to the cache.
	Pull(cfg Config, supported int) (Resolved, error)
}

// gitSource is the default Source: it clones a git ref of the rules repo into
// the cache. This is the historical behavior and stays the default until the
// signed-channel cutover (ENG-6).
type gitSource struct{}

// Default is the Source used when a Config selects no other (Channel empty).
var Default Source = gitSource{}

// SourceFor selects the Source a Config asks for. A non-empty Channel routes to
// the signed releaseSource; otherwise the default gitSource is used.
func SourceFor(cfg Config) Source {
	if cfg.Channel != "" {
		return newReleaseSource()
	}
	return Default
}

// Resolve resolves a rule pack using the Source selected by cfg. Behavior for a
// git-backed Config is unchanged from when this was the only path.
func Resolve(cfg Config, supported int) (Resolved, error) {
	return SourceFor(cfg).Resolve(cfg, supported)
}

// Pull fetches a rule pack using the Source selected by cfg.
func Pull(cfg Config, supported int) (Resolved, error) {
	return SourceFor(cfg).Pull(cfg, supported)
}

// withDefaults returns cfg with empty fields filled in.
func withDefaults(cfg Config) (Config, error) {
	if cfg.RepoURL == "" {
		cfg.RepoURL = DefaultRepoURL
	}
	if cfg.CacheDir == "" || cfg.BundleCacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return cfg, fmt.Errorf("locate user cache dir: %w", err)
		}
		if cfg.CacheDir == "" {
			cfg.CacheDir = filepath.Join(base, "trustabl", "rules")
		}
		if cfg.BundleCacheDir == "" {
			// A sibling of the rules cache, never under it, so no rules-cache
			// pruner can reach it (see Config.BundleCacheDir).
			cfg.BundleCacheDir = filepath.Join(base, "trustabl", "bundles")
		}
	}
	return cfg, nil
}

// usePack builds a Resolved for an already-cached SHA. It rejects only a pack
// with no usable manifest (missing / unparseable / non-positive version). A
// pack whose version merely EXCEEDS `supported` is accepted and flagged
// SchemaNewer — the lenient loader degrades it gracefully rather than refusing.
func usePack(cfg Config, sha string, fromCache bool, supported int) (Resolved, error) {
	fsys := os.DirFS(packDir(cfg.CacheDir, sha))
	mi := readManifestInfo(fsys)
	if !mi.valid {
		return Resolved{}, ErrNoCompatibleRules
	}
	return Resolved{
		FS:            fsys,
		SHA:           sha,
		RepoURL:       cfg.RepoURL,
		FromCache:     fromCache,
		SchemaVersion: mi.version,
		SchemaNewer:   mi.version > supported,
	}, nil
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
// story) or an unusable freshly-fetched manifest, but a local install fault — disk full,
// permission denied, a failed rename, or a corrupt clone (a fatalResolveError) —
// is propagated, never masked by stale cached rules. It returns ErrNoRules only
// when nothing is available at all.
func (gitSource) Resolve(cfg Config, supported int) (Resolved, error) {
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
		// Pack fetched but unusable (no valid manifest). Try the last
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
func (gitSource) Pull(cfg Config, supported int) (Resolved, error) {
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
