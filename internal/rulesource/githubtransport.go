package rulesource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/rulesign"
)

// Bundle/statement download caps. A statement is a tiny JSON document; a rules
// bundle is a few hundred KiB of YAML. The ceilings bound a hostile or
// misconfigured endpoint — an unbounded body or a decompression bomb — well
// above any real artifact.
const (
	maxStatementBytes = 1 << 20   // 1 MiB
	maxBundleBytes    = 64 << 20  // 64 MiB compressed
	maxBundleUnpacked = 256 << 20 // 256 MiB unpacked
	maxBundleEntries  = 100_000
)

// githubTransport fetches channel artifacts from GitHub Releases.
//
// Asset layout (the ENG-4 ⇄ RUL-4 contract — RUL-4's publish/promote workflows
// MUST produce exactly this so an engine build can resolve it):
//
//	channel statement: <repo>/releases/download/channel-<name>/statement.json
//	    A release tagged `channel-<name>` whose `statement.json` asset is the
//	    signed pointer. Promote re-points by updating this release's asset; the
//	    download URL stays stable.
//	bundle:            <repo>/releases/download/bundle-<digest>/bundle.tar.gz
//	    An immutable release tagged `bundle-<digest>` carrying the gzipped tar
//	    of the bundle. Content-addressed: the tag encodes the digest the
//	    statement commits to.
type githubTransport struct {
	client httpGetter
}

// httpGetter is the minimal HTTP surface the transport needs, narrowed so a
// test can substitute a fake without a real server.
type httpGetter interface {
	Get(url string) (*http.Response, error)
}

func newGitHubTransport() *githubTransport {
	return &githubTransport{client: &http.Client{Timeout: networkTimeout}}
}

func (t *githubTransport) FetchStatement(repoURL, channel string) ([]byte, error) {
	return t.get(statementURL(repoURL, channel), maxStatementBytes)
}

func (t *githubTransport) FetchBundle(repoURL, digest string) (fs.FS, error) {
	raw, err := t.get(bundleURL(repoURL, digest), maxBundleBytes)
	if err != nil {
		return nil, err
	}
	return untarGz(raw)
}

// repoWebBase normalizes a rules-repo URL to its web base (no trailing slash,
// no ".git"), the prefix GitHub release-asset download URLs hang off.
func repoWebBase(repoURL string) string {
	return strings.TrimSuffix(strings.TrimSuffix(repoURL, "/"), ".git")
}

func statementURL(repoURL, channel string) string {
	return repoWebBase(repoURL) + "/releases/download/channel-" + channel + "/statement.json"
}

func bundleURL(repoURL, digest string) string {
	return repoWebBase(repoURL) + "/releases/download/bundle-" + digest + "/bundle.tar.gz"
}

// get fetches url, enforcing a 200 status and a hard byte ceiling so a hostile
// endpoint cannot stream an unbounded body into memory.
func (t *githubTransport) get(url string, maxBytes int64) ([]byte, error) {
	resp, err := t.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", url, maxBytes)
	}
	return data, nil
}

// untarGz decompresses and unpacks a gzipped tar into an in-memory filesystem,
// rejecting unsafe paths and bounding total size and entry count. The result is
// untrusted until releaseSource matches its CanonicalDigest to the statement.
func untarGz(raw []byte) (fs.FS, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decompress bundle: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	out := fstest.MapFS{}
	seenLower := map[string]bool{}
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read bundle tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // skip dirs, symlinks, devices
		}
		name := path.Clean(hdr.Name)
		if !fs.ValidPath(name) || name == "." {
			return nil, fmt.Errorf("bundle contains unsafe path %q", hdr.Name)
		}
		// Reject names fs.ValidPath accepts but that reinterpret across platforms
		// (backslash, colon/drive-letter, trailing dot/space, reserved device name,
		// control char) — the SAME check the producer applies — so the on-disk
		// install can never diverge from the verified digest.
		if err := rulesign.ValidateBundlePath(name); err != nil {
			return nil, fmt.Errorf("bundle contains non-portable path %q: %w", hdr.Name, err)
		}
		lc := strings.ToLower(name)
		if seenLower[lc] {
			return nil, fmt.Errorf("bundle has case-folding-colliding paths (near %q) that would collapse to one file on a case-insensitive filesystem", hdr.Name)
		}
		seenLower[lc] = true
		if len(out) >= maxBundleEntries {
			return nil, fmt.Errorf("bundle exceeds %d entries", maxBundleEntries)
		}
		remaining := int64(maxBundleUnpacked) - total
		if remaining <= 0 {
			return nil, fmt.Errorf("bundle exceeds %d bytes unpacked", maxBundleUnpacked)
		}
		data, err := io.ReadAll(io.LimitReader(tr, remaining+1))
		if err != nil {
			return nil, fmt.Errorf("read bundle entry %q: %w", name, err)
		}
		if int64(len(data)) > remaining {
			return nil, fmt.Errorf("bundle exceeds %d bytes unpacked", maxBundleUnpacked)
		}
		total += int64(len(data))
		out[name] = &fstest.MapFile{Data: data}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bundle is empty")
	}
	return out, nil
}
