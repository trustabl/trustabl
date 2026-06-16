// Package rulesign is the engine-side trust core for distributed rule bundles:
// it computes a bundle's canonical content digest and verifies Ed25519
// signatures against an embedded trust keyring. It is deliberately
// verify-only — no private-key material or signing code ships in the scanner
// binary; signing happens in CI against KMS-held keys (RUL-4). Every
// verification fails closed: an unknown key, an out-of-window key, or a bad
// signature returns a typed error and never a "trusted" result.
package rulesign

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// canonicalEpoch is the fixed modification time stamped on every tar entry.
// Bundle identity must be a pure function of (paths × contents), so real
// filesystem timestamps are discarded. The Unix epoch (not the zero Time, which
// USTAR cannot encode) keeps the header byte-stable.
var canonicalEpoch = time.Unix(0, 0).UTC()

// CanonicalDigest returns the lowercase hex sha256 of a deterministic tar
// serialization of fsys. The same set of regular files and contents always
// yields the same digest regardless of walk order, on-disk timestamps, or
// ownership — the property that lets a publisher (RUL-4) and this engine
// independently arrive at the identical bundle identity, and lets a channel
// statement bind to a bundle by digest alone. Directories and non-regular
// files do not contribute; only regular-file paths and their bytes do.
func CanonicalDigest(fsys fs.FS) (string, error) {
	h := sha256.New()
	if err := WriteCanonicalTar(h, fsys); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WriteCanonicalTar streams a normalized USTAR archive of fsys into w. Entries
// are emitted in lexicographic path order; every header field that could carry
// nondeterminism (mtime, uid/gid, user/group names, access/change times) is
// fixed, so the byte stream — and thus its hash — depends only on the sorted
// (path, content) pairs.
//
// It is exported so a bundle publisher (RUL-4) can write the EXACT artifact the
// engine will re-derive a digest from: gzip this stream into bundle.tar.gz and
// the engine's CanonicalDigest of the unpacked files reproduces the same digest
// the producer computed. Both producer and verifier therefore share one
// serialization and can never drift on tar encoding.
func WriteCanonicalTar(w io.Writer, fsys fs.FS) error {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	// Reject paths that fs.ValidPath accepts but that reinterpret across platforms
	// (backslash, drive letter/colon, trailing dot/space, reserved device name,
	// control char) or case-fold-collide with another entry. The digest is computed
	// over this in-memory set, but the engine installs the files to disk; an
	// ambiguous name would make the on-disk tree diverge from the verified digest
	// (e.g. two case-colliding entries collapse to one file on a case-insensitive
	// FS). Refusing here keeps "the installed tree == the signed digest" true on
	// every target platform. The consumer (untarGz) applies the identical check.
	if err := validateBundlePaths(paths); err != nil {
		return err
	}
	// A bundle with no regular files is invalid — the consumer (untarGz) already
	// rejects an empty archive, so the producer must too, or the two sides
	// disagree on what a valid bundle is.
	if len(paths) == 0 {
		return fmt.Errorf("rulesign: bundle has no regular files")
	}

	tw := tar.NewWriter(w)
	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		// USTAR with all variable fields pinned. A path too long for USTAR
		// (>100 bytes without a ≤155-byte prefix split) makes WriteHeader fail
		// loudly rather than silently change formats — RUL-1 keeps bundle paths
		// within this bound.
		hdr := &tar.Header{
			Name:    p,
			Mode:    0o644,
			Size:    int64(len(data)),
			ModTime: canonicalEpoch,
			Format:  tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			// The most likely cause is a path too long for USTAR (name >100 bytes
			// with no ≤155-byte prefix split). Make that explicit and author-facing
			// rather than surfacing a bare archive/tar error.
			return fmt.Errorf("rulesign: bundle path %q cannot be encoded (USTAR limit: a path component name must be ≤100 bytes, optionally with a ≤155-byte directory prefix): %w", p, err)
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return tw.Close()
}

// ValidateBundlePath reports whether name is a portable, unambiguous bundle path.
// fs.ValidPath accepts names that reinterpret on Windows/macOS — a backslash
// (separator), a drive letter or any colon, a trailing dot/space (Windows strips
// these), a reserved device basename (CON, PRN, AUX, NUL, COM1-9, LPT1-9), or a
// control character. Such a name would install to a different on-disk path than
// the digest describes. Both the producer (WriteCanonicalTar) and the consumer
// (githubtransport.untarGz) route every entry through this one helper so they
// cannot drift on what a "valid" bundle path is.
func ValidateBundlePath(name string) error {
	if name == "" {
		return fmt.Errorf("rulesign: empty bundle path")
	}
	if strings.ContainsRune(name, '\\') {
		return fmt.Errorf("rulesign: bundle path %q contains a backslash (not portable)", name)
	}
	if strings.ContainsRune(name, ':') {
		return fmt.Errorf("rulesign: bundle path %q contains a colon / drive letter (not portable)", name)
	}
	for _, comp := range strings.Split(name, "/") {
		if comp == "" {
			continue
		}
		for _, r := range comp {
			if r < 0x20 {
				return fmt.Errorf("rulesign: bundle path %q contains a control character", name)
			}
		}
		if comp[0] == '~' {
			return fmt.Errorf("rulesign: bundle path component %q begins with '~' (home-expansion hazard)", comp)
		}
		if last := comp[len(comp)-1]; last == '.' || last == ' ' {
			return fmt.Errorf("rulesign: bundle path component %q ends with a dot or space (Windows strips these)", comp)
		}
		base := comp
		if i := strings.IndexByte(comp, '.'); i >= 0 {
			base = comp[:i]
		}
		if isReservedDeviceName(base) {
			return fmt.Errorf("rulesign: bundle path component %q is a reserved device name", comp)
		}
	}
	return nil
}

// isReservedDeviceName reports whether s (a path component's basename, sans
// extension) is a Windows reserved device name, case-insensitively.
func isReservedDeviceName(s string) bool {
	switch strings.ToUpper(s) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	up := strings.ToUpper(s)
	if len(up) == 4 && (strings.HasPrefix(up, "COM") || strings.HasPrefix(up, "LPT")) && up[3] >= '1' && up[3] <= '9' {
		return true
	}
	return false
}

// validateBundlePaths applies ValidateBundlePath to every entry and rejects any
// case-fold collision (two distinct entries that map to one file on a
// case-insensitive filesystem).
func validateBundlePaths(paths []string) error {
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if err := ValidateBundlePath(p); err != nil {
			return err
		}
		lc := strings.ToLower(p)
		if seen[lc] {
			return fmt.Errorf("rulesign: bundle has case-folding-colliding paths (near %q) that would collapse to one file on a case-insensitive filesystem", p)
		}
		seen[lc] = true
	}
	return nil
}
