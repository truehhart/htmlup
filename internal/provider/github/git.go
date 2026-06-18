package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path"

	"github.com/google/go-github/v72/github"
	"golang.org/x/sync/errgroup"
)

// pushCommit creates blobs for every entry, builds a tree on top of the
// branch's current state (or a fresh tree if the branch is missing), commits
// it, and points the branch ref at the new commit. It creates the branch if it
// does not already exist. Both the publish and setup flows share this logic.
func pushCommit(
	ctx context.Context,
	client *github.Client,
	owner, repo, branch, message string,
	entries []fileEntry,
	verbose bool,
) (*github.Commit, error) {
	treeEntries := make([]*github.TreeEntry, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for i, e := range entries {
		g.Go(func() error {
			data, err := e.read()
			if err != nil {
				return fmt.Errorf("reading %s: %w", e.path, err)
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "creating blob: %s (%d bytes)\n", e.path, len(data))
			}
			blob, _, err := client.Git.CreateBlob(gctx, owner, repo, &github.Blob{
				Content:  github.Ptr(base64.StdEncoding.EncodeToString(data)),
				Encoding: github.Ptr("base64"),
			})
			if err != nil {
				return fmt.Errorf("creating blob for %s: %w", e.path, err)
			}
			treeEntries[i] = &github.TreeEntry{
				Path: github.Ptr(e.path),
				Mode: github.Ptr("100644"),
				Type: github.Ptr("blob"),
				SHA:  blob.SHA,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	ref, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	var branchExists bool
	if err != nil {
		if !is404(err) {
			return nil, fmt.Errorf("checking branch %s: %w", branch, err)
		}
	} else {
		branchExists = true
	}

	var baseTree string
	var parents []*github.Commit
	var baseCommit *github.Commit
	if branchExists {
		commitSHA := ref.Object.GetSHA()
		commit, _, err := client.Git.GetCommit(ctx, owner, repo, commitSHA)
		if err != nil {
			return nil, fmt.Errorf("getting commit %s: %w", commitSHA, err)
		}
		baseCommit = commit
		baseTree = commit.Tree.GetSHA()
		parents = []*github.Commit{{SHA: github.Ptr(commitSHA)}}
	}

	tree, _, err := client.Git.CreateTree(ctx, owner, repo, baseTree, treeEntries)
	if err != nil {
		return nil, fmt.Errorf("creating tree: %w", err)
	}

	// Nothing changed — skip the commit so we don't push an empty commit and
	// needlessly re-trigger a Pages build.
	if branchExists && tree.GetSHA() == baseTree {
		if verbose {
			fmt.Fprintf(os.Stderr, "no changes on %s; skipping commit\n", branch)
		}
		return baseCommit, nil
	}

	newCommit, _, err := client.Git.CreateCommit(ctx, owner, repo, &github.Commit{
		Message: github.Ptr(message),
		Tree:    tree,
		Parents: parents,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("creating commit: %w", err)
	}

	if branchExists {
		ref.Object.SHA = newCommit.SHA
		_, _, err = client.Git.UpdateRef(ctx, owner, repo, ref, false)
	} else {
		_, _, err = client.Git.CreateRef(ctx, owner, repo, &github.Reference{
			Ref:    github.Ptr("refs/heads/" + branch),
			Object: &github.GitObject{SHA: newCommit.SHA},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("updating branch ref: %w", err)
	}

	return newCommit, nil
}

type fileEntry struct {
	path string
	// read returns the entry's bytes. publish reads from the source fs lazily at
	// blob-creation time (see collectFiles) so a large tree isn't held in memory
	// all at once; setup supplies already-materialized content via staticContent.
	read func() ([]byte, error)
}

// staticContent adapts already-materialized bytes — setup's synthesized landing
// page, CNAME, and workflow — to the lazy fileEntry.read contract.
func staticContent(b []byte) func() ([]byte, error) {
	return func() ([]byte, error) { return b, nil }
}

// collectFiles enumerates the tree into entries that read their bytes lazily.
// Only the path list is materialized here; each file's content is read inside
// pushCommit's bounded upload loop, so peak memory is ~concurrency×file rather
// than the whole tree. (GitHub's blob API still needs each individual file
// fully in memory to base64-encode it — that per-file floor is unavoidable.)
func collectFiles(files fs.FS, dir string) ([]fileEntry, error) {
	var entries []fileEntry
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		remotePath := p
		if dir != "" {
			remotePath = path.Join(dir, p)
		}
		// p is the callback's own parameter (not a shared loop variable), so the
		// closure safely captures this entry's source path.
		entries = append(entries, fileEntry{
			path: remotePath,
			read: func() ([]byte, error) { return fs.ReadFile(files, p) },
		})
		return nil
	})
	return entries, err
}
