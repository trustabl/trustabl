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
	"io"
	"io/fs"
	"sort"
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
	if err := writeCanonicalTar(h, fsys); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeCanonicalTar streams a normalized USTAR archive of fsys into w. Entries
// are emitted in lexicographic path order; every header field that could carry
// nondeterminism (mtime, uid/gid, user/group names, access/change times) is
// fixed, so the byte stream — and thus its hash — depends only on the sorted
// (path, content) pairs.
func writeCanonicalTar(w io.Writer, fsys fs.FS) error {
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
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return tw.Close()
}
