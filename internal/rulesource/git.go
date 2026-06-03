package rulesource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// networkTimeout bounds every remote git operation (ref listing and clone). A
// hung or slow-loris remote must not hang the scan forever — without a deadline
// the only way out is SIGINT. 120s is generous for a shallow clone of the rules
// repo while still guaranteeing forward progress.
const networkTimeout = 120 * time.Second

// resolveRef contacts the remote at url and returns the commit SHA and full
// reference name for ref. An empty ref resolves the remote's default-branch
// HEAD; a non-empty ref is matched against branches then tags. The operation is
// bounded by ctx (see networkTimeout).
func resolveRef(ctx context.Context, url, ref string) (sha string, name plumbing.ReferenceName, err error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := remote.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("contact rules repo %s: %w", url, err)
	}

	if ref == "" {
		// HEAD may come back as a hash ref directly or as a symbolic ref
		// whose target we resolve among the listed refs.
		var headTarget plumbing.ReferenceName
		for _, r := range refs {
			if r.Name() == plumbing.HEAD {
				if r.Type() == plumbing.HashReference {
					return r.Hash().String(), "", nil
				}
				headTarget = r.Target()
			}
		}
		for _, r := range refs {
			if r.Name() == headTarget {
				return r.Hash().String(), "", nil
			}
		}
		return "", "", fmt.Errorf("rules repo %s: could not resolve HEAD", url)
	}

	branch := plumbing.NewBranchReferenceName(ref)
	tag := plumbing.NewTagReferenceName(ref)
	for _, r := range refs {
		if r.Name() == branch || r.Name() == tag {
			return r.Hash().String(), r.Name(), nil
		}
	}
	return "", "", fmt.Errorf("rules repo %s: ref %q not found", url, ref)
}

// cloneInto shallow-clones url at refName into a temp directory under cacheDir,
// then atomically renames it to the pack directory named by the actual cloned
// HEAD commit. It returns that commit SHA. An empty refName clones the default
// branch.
//
// Two correctness properties hang on the temp-dir-then-rename:
//   - An interrupted clone (process kill, power loss, disk full) leaves only a
//     `.tmp-clone-*` dir, never a half-written `<sha>/` pack that packExists
//     would treat as complete. The rename is the commit point.
//   - The pack is named by the commit it actually contains, read from the
//     cloned repo's HEAD — not by the SHA resolveRef observed earlier. That
//     closes the window where a branch tip advances between resolveRef and the
//     clone, which would otherwise record a SHA that mislabels the content.
func cloneInto(ctx context.Context, url string, refName plumbing.ReferenceName, cacheDir string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", &fatalResolveError{fmt.Errorf("create rules cache dir: %w", err)}
	}
	tmp, err := os.MkdirTemp(cacheDir, ".tmp-clone-*")
	if err != nil {
		return "", &fatalResolveError{fmt.Errorf("create rules clone temp dir: %w", err)}
	}
	defer os.RemoveAll(tmp) // no-op once a successful rename moves the contents

	opts := &git.CloneOptions{URL: url, Depth: 1, SingleBranch: true}
	if refName != "" {
		opts.ReferenceName = refName
	}
	// A clone failure is a remote-contact fault — left unwrapped so Resolve may
	// fall back to cached rules (the offline story). The clone is ctx-bounded so
	// a hung remote cannot stall the scan indefinitely.
	repo, err := git.PlainCloneContext(ctx, tmp, false, opts)
	if err != nil {
		return "", fmt.Errorf("clone rules repo %s: %w", url, err)
	}
	// Past this point the bytes are local: a HEAD-resolution failure means the
	// freshly cloned repo is corrupt, which must not silently serve stale rules.
	head, err := repo.Head()
	if err != nil {
		return "", &fatalResolveError{fmt.Errorf("resolve cloned HEAD for %s: %w", url, err)}
	}
	sha := head.Hash().String()

	dest := packDir(cacheDir, sha)
	// A concurrent process may have already materialized this exact pack. A
	// COMPLETE pack is immutable once named by its commit, so keep it and
	// discard our temp clone. A dest that exists WITHOUT the completeness marker
	// is a partial pack (e.g. an interrupted prune) — remove it so our fresh,
	// complete clone can take its place rather than being silently trusted.
	if packExists(cacheDir, sha) {
		return sha, nil
	}
	_ = os.RemoveAll(dest)
	// Drop the .git tree before installing the pack: the cached pack only needs
	// the tracked rule files, not the full clone history. Leaving .git in place
	// bloats every cached SHA and ships VCS plumbing that the loader would have
	// to defend against. Best-effort — if go-git still holds open handles (can
	// happen on Windows), the loader independently skips any .git/ subtree, so a
	// residual .git never poisons rule loading.
	_ = os.RemoveAll(filepath.Join(tmp, ".git"))
	// Write the sentinel into the temp clone as the LAST step before the rename,
	// so the installed pack atomically appears complete (dir + marker together).
	if err := markPackComplete(tmp); err != nil {
		return "", &fatalResolveError{fmt.Errorf("mark rules pack complete: %w", err)}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if packExists(cacheDir, sha) {
			return sha, nil // lost the rename race; existing complete pack is the same commit
		}
		return "", &fatalResolveError{fmt.Errorf("install rules pack: %w", err)}
	}
	return sha, nil
}
