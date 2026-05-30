package ingestion

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// CloneProgress receives remote-clone progress. The CLI's progress.Reporter
// satisfies it structurally; callers pass nil for no progress.
type CloneProgress interface {
	SetTotal(n int)          // total objects to receive (once, when known)
	Advance(detail string)   // one object received
	SetDetail(detail string) // a status line with no count (connecting, writing files)
	ResetPhase()             // clear a partial receiving-objects bar (the plumbing fetch failed and we're retrying via an uncounted clone)
}

// fetchTreeToDir shallow-fetches remoteURL's default branch (HEAD, depth 1) and
// writes its working tree into dir. Unlike go-git's high-level PlainClone, it
// drives the fetch at the plumbing level so it can observe the packfile parse
// and report an accurate "receiving objects N/M" bar — the phase PlainClone's
// Progress writer never surfaces.
//
// It writes only regular/executable file blobs; symlinks and submodules are
// skipped (the scanner reads source files, and skipping symlinks avoids
// path-escape risk). prog may be nil.
func fetchTreeToDir(ctx context.Context, remoteURL, dir string, prog CloneProgress) error {
	ep, err := transport.NewEndpoint(remoteURL)
	if err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	cli, err := client.NewClient(ep)
	if err != nil {
		return fmt.Errorf("transport client: %w", err)
	}
	// Auth is nil: works for public HTTPS. Private/SSH targets fail here and the
	// caller falls back to go-git's PlainClone (which uses its default auth).
	sess, err := cli.NewUploadPackSession(ep, nil)
	if err != nil {
		return fmt.Errorf("upload-pack session: %w", err)
	}
	if prog != nil {
		prog.SetDetail("connecting…")
	}
	ar, err := sess.AdvertisedReferencesContext(ctx)
	if err != nil {
		return fmt.Errorf("advertised refs: %w", err)
	}
	if ar.Head == nil {
		return fmt.Errorf("remote advertised no HEAD")
	}
	want := *ar.Head

	req := packp.NewUploadPackRequest()
	for _, c := range []capability.Capability{capability.OFSDelta, capability.Shallow} {
		if ar.Capabilities.Supports(c) {
			_ = req.Capabilities.Set(c)
		}
	}
	req.Wants = []plumbing.Hash{want}
	req.Depth = packp.DepthCommits(1)

	resp, err := sess.UploadPack(ctx, req)
	if err != nil {
		return fmt.Errorf("upload-pack: %w", err)
	}
	defer resp.Close()

	// Store objects under dir/.git (recon skips .git; the temp dir is removed
	// wholesale on cleanup). Filesystem storage bounds memory on large repos.
	storage := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	scanner := packfile.NewScanner(resp)
	obs := &cloneObserver{prog: prog}
	parser, err := packfile.NewParserWithStorage(scanner, storage, obs)
	if err != nil {
		return fmt.Errorf("packfile parser: %w", err)
	}
	if _, err := parser.Parse(); err != nil {
		return fmt.Errorf("parse packfile: %w", err)
	}

	// Walk the wanted commit's tree and write the working-tree files.
	if prog != nil {
		prog.SetDetail("writing files…")
	}
	commit, err := object.GetCommit(storage, want)
	if err != nil {
		return fmt.Errorf("read commit %s: %w", want, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("read tree: %w", err)
	}
	return tree.Files().ForEach(func(f *object.File) error {
		if f.Mode == filemode.Symlink || f.Mode == filemode.Submodule {
			return nil // not source we parse; skip (also avoids symlink escapes)
		}
		dst, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		return writeBlob(dst, f)
	})
}

// cloneObserver turns packfile parse callbacks into clone progress: OnHeader
// fixes the total, each object header advances the bar.
type cloneObserver struct{ prog CloneProgress }

func (o *cloneObserver) OnHeader(count uint32) error {
	if o.prog != nil {
		o.prog.SetTotal(int(count))
	}
	return nil
}
func (o *cloneObserver) OnInflatedObjectHeader(plumbing.ObjectType, int64, int64) error {
	if o.prog != nil {
		o.prog.Advance("receiving objects")
	}
	return nil
}
func (o *cloneObserver) OnInflatedObjectContent(plumbing.Hash, int64, uint32, []byte) error {
	return nil
}
func (o *cloneObserver) OnFooter(plumbing.Hash) error { return nil }

// writeBlob writes file f's contents to dst, creating parent dirs, with the
// file's mode (best-effort; mode errors are non-fatal on Windows).
func writeBlob(dst string, f *object.File) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Reader()
	if err != nil {
		return fmt.Errorf("read blob %s: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if m, err := f.Mode.ToOSFileMode(); err == nil {
		_ = os.Chmod(dst, m)
	}
	return nil
}

// safeJoin joins a repo-relative tree path onto root, rejecting any path that
// would escape root (absolute, or climbing via ..). Tree paths use forward
// slashes regardless of OS.
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute path in tree: %q", rel)
	}
	joined := filepath.Join(root, clean)
	// joined must be root or strictly within it.
	relCheck, err := filepath.Rel(root, joined)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", rel, err)
	}
	if relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes clone dir: %q", rel)
	}
	return joined, nil
}
