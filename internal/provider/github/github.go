package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v72/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"

	"github.com/truehhart/htmlupclaude/internal/fsutil"
	"github.com/truehhart/htmlupclaude/internal/provider"
)

func init() {
	provider.Register(&Provider{})
}

type Provider struct {
	repo   string
	branch string
	dir    string
	cname  string
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub Pages operations",
	}
	cmd.AddCommand(p.publishCmd())
	return cmd
}

func (p *Provider) publishCmd() *cobra.Command {
	var (
		dryRun  bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "publish <path>",
		Short: "Publish HTML to GitHub Pages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := p.validate(); err != nil {
				return err
			}
			files, err := fsutil.ResolveFS(args[0])
			if err != nil {
				return err
			}
			result, err := p.publish(cmd.Context(), provider.Target{
				Files:   files,
				DryRun:  dryRun,
				Verbose: verbose,
			})
			if err != nil {
				return err
			}
			fmt.Println(result.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.repo, "repo", "", "target repository (owner/name)")
	_ = cmd.MarkFlagRequired("repo")
	cmd.Flags().StringVar(&p.branch, "branch", "gh-pages", "branch to push to")
	cmd.Flags().StringVar(&p.dir, "dir", "", "subdirectory within the branch")
	cmd.Flags().StringVar(&p.cname, "cname", "", "custom domain (writes CNAME file)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
}

func (p *Provider) validate() error {
	parts := strings.SplitN(p.repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--repo must be in owner/name format")
	}
	return nil
}

func (p *Provider) publish(ctx context.Context, t provider.Target) (provider.Result, error) {
	owner, repoName := p.ownerRepo()

	token, err := resolveToken(ctx)
	if err != nil {
		return provider.Result{}, err
	}

	client := github.NewClient(
		oauth2.NewClient(ctx, oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)),
	)

	entries, err := collectFiles(t.Files, p.dir)
	if err != nil {
		return provider.Result{}, fmt.Errorf("reading files: %w", err)
	}
	if p.cname != "" {
		entries = append(entries, fileEntry{path: "CNAME", content: []byte(p.cname + "\n")})
	}
	if len(entries) == 0 {
		return provider.Result{}, fmt.Errorf("no files to publish")
	}

	url := p.pagesURL(owner, repoName)

	if t.DryRun {
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "would upload: %s\n", e.path)
		}
		fmt.Fprintf(os.Stderr, "target: %s branch %s (%d files)\n", p.repo, p.branch, len(entries))
		return provider.Result{URL: url}, nil
	}

	treeEntries := make([]*github.TreeEntry, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for i, e := range entries {
		g.Go(func() error {
			if t.Verbose {
				fmt.Fprintf(os.Stderr, "creating blob: %s (%d bytes)\n", e.path, len(e.content))
			}
			blob, _, err := client.Git.CreateBlob(gctx, owner, repoName, &github.Blob{
				Content:  github.Ptr(base64.StdEncoding.EncodeToString(e.content)),
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
		return provider.Result{}, err
	}

	ref, _, err := client.Git.GetRef(ctx, owner, repoName, "refs/heads/"+p.branch)
	var branchExists bool
	if err != nil {
		var ghErr *github.ErrorResponse
		if !errors.As(err, &ghErr) || ghErr.Response.StatusCode != 404 {
			return provider.Result{}, fmt.Errorf("checking branch %s: %w", p.branch, err)
		}
	} else {
		branchExists = true
	}

	var baseTree string
	var parents []*github.Commit
	if branchExists {
		commitSHA := ref.Object.GetSHA()
		commit, _, err := client.Git.GetCommit(ctx, owner, repoName, commitSHA)
		if err != nil {
			return provider.Result{}, fmt.Errorf("getting commit %s: %w", commitSHA, err)
		}
		baseTree = commit.Tree.GetSHA()
		parents = []*github.Commit{{SHA: github.Ptr(commitSHA)}}
	}

	tree, _, err := client.Git.CreateTree(ctx, owner, repoName, baseTree, treeEntries)
	if err != nil {
		return provider.Result{}, fmt.Errorf("creating tree: %w", err)
	}

	newCommit, _, err := client.Git.CreateCommit(ctx, owner, repoName, &github.Commit{
		Message: github.Ptr("publish via htmlup"),
		Tree:    tree,
		Parents: parents,
	}, nil)
	if err != nil {
		return provider.Result{}, fmt.Errorf("creating commit: %w", err)
	}

	if branchExists {
		ref.Object.SHA = newCommit.SHA
		_, _, err = client.Git.UpdateRef(ctx, owner, repoName, ref, false)
	} else {
		_, _, err = client.Git.CreateRef(ctx, owner, repoName, &github.Reference{
			Ref:    github.Ptr("refs/heads/" + p.branch),
			Object: &github.GitObject{SHA: newCommit.SHA},
		})
	}
	if err != nil {
		return provider.Result{}, fmt.Errorf("updating branch ref: %w", err)
	}

	if t.Verbose {
		fmt.Fprintf(os.Stderr, "commit: %s\n", newCommit.GetSHA())
	}

	p.ensurePages(ctx, client, owner, repoName)

	if t.Verbose {
		fmt.Fprintf(os.Stderr, "published %d files to %s\n", len(entries), url)
	}

	return provider.Result{URL: url}, nil
}

func (p *Provider) ownerRepo() (string, string) {
	parts := strings.SplitN(p.repo, "/", 2)
	return parts[0], parts[1]
}

func (p *Provider) pagesURL(owner, repo string) string {
	var u string
	switch {
	case p.cname != "":
		u = "https://" + p.cname + "/"
	case repo == owner+".github.io":
		u = "https://" + owner + ".github.io/"
	default:
		u = "https://" + owner + ".github.io/" + repo + "/"
	}
	if p.dir != "" {
		u += p.dir + "/"
	}
	return u
}

func (p *Provider) ensurePages(ctx context.Context, client *github.Client, owner, repo string) {
	_, _, err := client.Repositories.GetPagesInfo(ctx, owner, repo)
	if err == nil {
		return
	}
	_, _, err = client.Repositories.EnablePages(ctx, owner, repo, &github.Pages{
		BuildType: github.Ptr("legacy"),
		Source: &github.PagesSource{
			Branch: github.Ptr(p.branch),
			Path:   github.Ptr("/"),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not enable GitHub Pages: %v\n", err)
	}
}

type fileEntry struct {
	path    string
	content []byte
}

func collectFiles(files fs.FS, dir string) ([]fileEntry, error) {
	var entries []fileEntry
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		remotePath := p
		if dir != "" {
			remotePath = path.Join(dir, p)
		}
		entries = append(entries, fileEntry{path: remotePath, content: data})
		return nil
	})
	return entries, err
}

func resolveToken(ctx context.Context) (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "gh", "auth", "token").Output()
	if err == nil {
		if token := strings.TrimSpace(string(out)); token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("no GitHub token found: set GITHUB_TOKEN, GH_TOKEN, or run 'gh auth login'")
}
