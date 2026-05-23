package rulesource

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// resolveRef contacts the remote at url and returns the commit SHA and full
// reference name for ref. An empty ref resolves the remote's default-branch
// HEAD; a non-empty ref is matched against branches then tags.
func resolveRef(url, ref string) (sha string, name plumbing.ReferenceName, err error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := remote.List(&git.ListOptions{})
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
func cloneInto(url string, refName plumbing.ReferenceName, cacheDir string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cacheDir, ".tmp-clone-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // no-op once a successful rename moves the contents

	opts := &git.CloneOptions{URL: url, Depth: 1, SingleBranch: true}
	if refName != "" {
		opts.ReferenceName = refName
	}
	repo, err := git.PlainClone(tmp, false, opts)
	if err != nil {
		return "", fmt.Errorf("clone rules repo %s: %w", url, err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("resolve cloned HEAD for %s: %w", url, err)
	}
	sha := head.Hash().String()

	dest := packDir(cacheDir, sha)
	// A concurrent process may have already materialized this exact pack. A
	// pack is immutable once named by its commit, so an existing dest is
	// authoritative — keep it and discard our temp clone.
	if _, statErr := os.Stat(dest); statErr == nil {
		return sha, nil
	}
	if err := os.Rename(tmp, dest); err != nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return sha, nil // lost the rename race; existing pack is the same commit
		}
		return "", fmt.Errorf("install rules pack: %w", err)
	}
	return sha, nil
}
