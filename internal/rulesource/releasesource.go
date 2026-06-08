package rulesource

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// ChannelTransport fetches the raw artifacts of the signed-distribution model:
// a channel's current signed statement, and a bundle's content by digest. It is
// the seam between releaseSource's verification pipeline and where the bytes
// actually come from — GitHub Releases in production (newGitHubTransport), an
// in-memory fake in tests. A transport performs NO verification; releaseSource
// owns all trust decisions.
type ChannelTransport interface {
	// FetchStatement returns the raw JSON of the channel's current signed
	// statement.
	FetchStatement(repoURL, channel string) ([]byte, error)
	// FetchBundle returns the contents of the bundle named by digest as a
	// read-only filesystem. The digest is NOT yet trusted — the caller
	// recomputes and compares it before using the bundle.
	FetchBundle(repoURL, digest string) (fs.FS, error)
}

// releaseSource resolves rules from a signed channel: it verifies a channel
// statement against the embedded trust keyring, fetches the bundle the
// statement commits to, re-derives and matches its digest, installs it to a
// content-addressed cache, and advances the channel's anti-rollback floor.
// Every trust failure refuses loudly; only a remote-contact failure degrades to
// the cached bundle (the offline story).
type releaseSource struct {
	keyring   *rulesign.Keyring
	transport ChannelTransport
	now       func() time.Time
}

// newReleaseSource builds the production releaseSource: the trust keyring
// embedded in this build plus the GitHub Releases transport. Until signing keys
// are published (RUL-2) the embedded keyring is empty, so every channel resolve
// refuses up front — fail-closed by construction.
func newReleaseSource() *releaseSource {
	ring, err := rulesign.Embedded()
	if err != nil {
		// An unparseable embedded keyring is a build defect; trust nothing
		// rather than panic at scan time.
		ring = rulesign.NewKeyring()
	}
	return &releaseSource{
		keyring:   ring,
		transport: newGitHubTransport(),
		now:       time.Now,
	}
}

// Resolve verifies and installs the channel's current bundle, falling back to
// the cached bundle only when the remote is unreachable.
func (rs *releaseSource) Resolve(cfg Config, supported int) (Resolved, error) {
	cfg, err := rs.prepare(cfg)
	if err != nil {
		return Resolved{}, err
	}
	if cfg.NoUpdate {
		return rs.fromCache(cfg, supported)
	}
	raw, err := rs.transport.FetchStatement(cfg.RepoURL, cfg.Channel)
	if err != nil {
		// Remote unreachable — degrade to the last verified bundle. A
		// verification failure is never routed here; that refuses below.
		return rs.fromCache(cfg, supported)
	}
	return rs.resolveFromStatement(cfg, supported, raw)
}

// Pull is the explicit-fetch path: it always contacts the remote and never
// falls back to the cache.
func (rs *releaseSource) Pull(cfg Config, supported int) (Resolved, error) {
	cfg, err := rs.prepare(cfg)
	if err != nil {
		return Resolved{}, err
	}
	raw, err := rs.transport.FetchStatement(cfg.RepoURL, cfg.Channel)
	if err != nil {
		return Resolved{}, fmt.Errorf("fetch channel statement: %w", err)
	}
	return rs.resolveFromStatement(cfg, supported, raw)
}

// prepare fills defaults, validates the repo URL, and refuses immediately if
// this build trusts no signing keys (an empty keyring would otherwise fail
// every statement as an unknown key — a clearer message up front).
func (rs *releaseSource) prepare(cfg Config) (Config, error) {
	cfg, err := withDefaults(cfg)
	if err != nil {
		return cfg, err
	}
	if err := validateRepoURL(cfg.RepoURL); err != nil {
		return cfg, err
	}
	if rs.keyring.Empty() {
		return cfg, fmt.Errorf("%w: the %q channel cannot be verified", ErrNoTrustKeys, cfg.Channel)
	}
	return cfg, nil
}

// resolveFromStatement runs the trust pipeline on a freshly-fetched statement:
// parse, verify (signature, channel, freshness, anti-rollback), fetch the
// bundle if not already cached, bind its digest, install, then advance the
// channel floor. Any verification failure returns its typed error — these are
// refusals, not fallbacks.
func (rs *releaseSource) resolveFromStatement(cfg Config, supported int, raw []byte) (Resolved, error) {
	stmt, err := rulesign.ParseStatement(raw)
	if err != nil {
		return Resolved{}, err
	}
	lastSeen, err := rulesign.ReadLastSeenVersion(cfg.BundleCacheDir, cfg.Channel)
	if err != nil {
		return Resolved{}, err
	}
	if err := rulesign.VerifyStatement(rs.keyring, stmt, rulesign.VerifyParams{
		Channel:         cfg.Channel,
		LastSeenVersion: lastSeen,
	}, rs.now()); err != nil {
		return Resolved{}, err
	}

	// Content-addressed cache: a bundle dir is named by its own digest, so if it
	// is already present its content already matched — no re-fetch, no re-verify.
	if !bundleExists(cfg.BundleCacheDir, stmt.Digest) {
		bundleFS, err := rs.transport.FetchBundle(cfg.RepoURL, stmt.Digest)
		if err != nil {
			return Resolved{}, fmt.Errorf("fetch bundle %s: %w", stmt.Digest, err)
		}
		got, err := rulesign.CanonicalDigest(bundleFS)
		if err != nil {
			return Resolved{}, err
		}
		if got != stmt.Digest {
			return Resolved{}, fmt.Errorf("%w: statement %s, downloaded %s", rulesign.ErrDigestMismatch, stmt.Digest, got)
		}
		if err := installBundle(cfg.BundleCacheDir, stmt.Digest, bundleFS); err != nil {
			return Resolved{}, &fatalResolveError{err}
		}
	}
	// Advance the anti-rollback floor only after a fully verified, installed
	// bundle — so a failed install can never move the floor forward.
	if _, err := rulesign.RecordStatement(cfg.BundleCacheDir, stmt); err != nil {
		return Resolved{}, &fatalResolveError{err}
	}
	return rs.usePackDigest(cfg, stmt.Digest, supported, false)
}

// fromCache serves the channel's last verified bundle when the network is
// skipped or unreachable, flagging it Stale if its statement has since expired.
func (rs *releaseSource) fromCache(cfg Config, supported int) (Resolved, error) {
	digest, _, expires, found, err := rulesign.ChannelPointer(cfg.BundleCacheDir, cfg.Channel)
	if err != nil {
		return Resolved{}, err
	}
	if !found || !bundleExists(cfg.BundleCacheDir, digest) {
		return Resolved{}, ErrNoRules
	}
	res, err := rs.usePackDigest(cfg, digest, supported, true)
	if err != nil {
		return Resolved{}, err
	}
	if expires != "" {
		if exp, perr := time.Parse(time.RFC3339, expires); perr == nil && rs.now().After(exp) {
			res.Stale = true
		}
	}
	return res, nil
}

// usePackDigest builds a Resolved over the installed bundle for digest, applying
// the same manifest schema gate as the git path.
func (rs *releaseSource) usePackDigest(cfg Config, digest string, supported int, fromCache bool) (Resolved, error) {
	fsys := os.DirFS(bundleDir(cfg.BundleCacheDir, digest))
	mi := readManifestInfo(fsys)
	if !mi.valid {
		return Resolved{}, ErrNoCompatibleRules
	}
	return Resolved{
		FS:            fsys,
		SHA:           digest,
		RepoURL:       cfg.RepoURL,
		FromCache:     fromCache,
		SchemaVersion: mi.version,
		SchemaNewer:   mi.version > supported,
	}, nil
}

// --- content-addressed bundle cache ------------------------------------------
//
// The signed cache root is Config.BundleCacheDir — a sibling of the git rules
// cache, never under it — so no rules-cache pruner can reach it. Within it,
// bundles live at <root>/<digest>/ and per-channel state at
// <root>/channels/<channel>.json.

// bundleDir is the cache directory for one bundle, named by its digest.
func bundleDir(bundleRoot, digest string) string {
	return filepath.Join(bundleRoot, digest)
}

// bundleExists reports whether a *complete* bundle for digest is cached — the
// directory exists and carries the completeness marker. A markerless directory
// is a partial install and is treated as absent.
func bundleExists(bundleRoot, digest string) bool {
	dir := bundleDir(bundleRoot, digest)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, completeMarker))
	return err == nil
}

// installBundle writes fsys into the content-addressed cache for digest. It
// materializes into a temp dir, marks it complete, then atomically renames it
// into place, so a concurrent reader never sees a half-written bundle.
func installBundle(bundleRoot, digest string, fsys fs.FS) error {
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(bundleRoot, ".tmp-bundle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) // no-op once the rename succeeds

	if err := writeFSTo(tmp, fsys); err != nil {
		return err
	}
	if err := markPackComplete(tmp); err != nil {
		return err
	}
	dest := bundleDir(bundleRoot, digest)
	// A concurrent resolve may have installed the identical (digest-named)
	// content first; that is success, not a conflict.
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	return os.Rename(tmp, dest)
}

// writeFSTo copies every regular file from fsys into root, recreating the
// directory structure. It refuses any entry whose path would escape root
// (defense in depth — fs.WalkDir already yields only valid relative paths) and
// skips non-regular files (symlinks, devices) that a bundle has no business
// carrying.
func writeFSTo(root string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		dest := filepath.Join(root, filepath.FromSlash(p))
		if !within(root, dest) {
			return fmt.Errorf("rulesource: unsafe bundle path %q", p)
		}
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// within reports whether p resolves inside root.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
