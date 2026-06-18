package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v72/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"

	"github.com/truehhart/htmlup/internal/fsutil"
	"github.com/truehhart/htmlup/internal/provider"
)

func init() {
	provider.Register(&Provider{})
}

type Provider struct {
	repo    string
	branch  string
	dir     string
	cname   string
	ttlDays int
	cron    string
	exclude []string
	noAuto  bool
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub Pages operations",
	}
	cmd.AddCommand(p.publishCmd())
	cmd.AddCommand(p.setupCmd())
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
			// Flags parsed cleanly; from here errors are runtime failures, not
			// misuse, so don't tack the usage screen onto them.
			cmd.SilenceUsage = true
			if err := p.validate(); err != nil {
				return err
			}
			files, err := fsutil.ResolveFS(args[0])
			if err != nil {
				return err
			}
			// By default, target wherever Pages already serves from. Setting
			// --branch/--dir explicitly (or --no-auto) opts back into manual mode.
			autoDetect := !p.noAuto && !cmd.Flags().Changed("branch") && !cmd.Flags().Changed("dir")
			result, err := p.publish(cmd.Context(), provider.Target{
				Files:   files,
				DryRun:  dryRun,
				Verbose: verbose,
			}, autoDetect)
			if err != nil {
				return err
			}
			result.PrintURLs()
			return nil
		},
	}
	cmd.Flags().StringVar(&p.repo, "repo", "", "target repository (owner/name)")
	_ = cmd.MarkFlagRequired("repo")
	cmd.Flags().StringVar(&p.branch, "branch", "gh-pages", "branch to push to (default: auto-detected from Pages settings)")
	cmd.Flags().StringVar(&p.dir, "dir", "", "subdirectory within the branch (default: auto-detected from Pages settings)")
	cmd.Flags().BoolVar(&p.noAuto, "no-auto", false, "don't auto-detect the target from GitHub Pages settings; use --branch/--dir as given")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be uploaded without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-file progress and SDK detail")
	return cmd
}

func (p *Provider) validate() error {
	if _, _, ok := splitRepo(p.repo); !ok {
		return fmt.Errorf("--repo must be in owner/name format")
	}
	if !validBranchName(p.branch) {
		return fmt.Errorf("--branch must be a valid Git branch name")
	}
	if !validPublishDir(p.dir) {
		return fmt.Errorf("--dir must be a clean relative path")
	}
	return nil
}

func validBranchName(branch string) bool {
	if branch == "" ||
		strings.HasPrefix(branch, "/") ||
		strings.HasSuffix(branch, "/") ||
		strings.HasSuffix(branch, ".") ||
		strings.Contains(branch, "//") ||
		strings.Contains(branch, "..") ||
		strings.Contains(branch, "@{") ||
		strings.ContainsAny(branch, " ~^:?*[\\") {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func validPublishDir(dir string) bool {
	if dir == "" {
		return true
	}
	if strings.HasPrefix(dir, "/") || strings.Contains(dir, "\\") || path.Clean(dir) != dir {
		return false
	}
	for _, part := range strings.Split(dir, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

// splitRepo parses an "owner/name" repo string. ok is false when either side is
// missing, which validate() turns into the user-facing error.
func splitRepo(repo string) (owner, name string, ok bool) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (p *Provider) publish(ctx context.Context, t provider.Target, autoDetect bool) (provider.Result, error) {
	owner, repoName := p.ownerRepo()

	token, err := resolveToken(ctx)
	if err != nil {
		return provider.Result{}, err
	}

	client := newGitHubClient(ctx, token)

	// Resolve the publish target into locals rather than mutating the receiver:
	// unless told otherwise, target wherever GitHub Pages already serves from
	// (its branch + source path), falling back to the flag values when Pages is
	// off or built from a workflow.
	branch, dir := p.branch, p.dir
	var autoURL string
	if autoDetect {
		if b, d, u, ok := p.autoTarget(ctx, client, owner, repoName); ok {
			branch, dir, autoURL = b, d, u
			if t.Verbose {
				fmt.Fprintf(os.Stderr, "auto-detected Pages source: branch %s, dir %q\n", branch, dir)
			}
		}
	}

	entries, err := collectFiles(t.Files, dir)
	if err != nil {
		return provider.Result{}, fmt.Errorf("reading files: %w", err)
	}
	if len(entries) == 0 {
		return provider.Result{}, fmt.Errorf("no files to publish")
	}

	// Report the served URL. A custom domain wins (read from an existing CNAME
	// file in the target — publish never writes one; that's `github setup`),
	// then the auto-detected Pages URL, then the github.io default. pushCommit
	// merges onto the branch's tree, so any existing CNAME is left untouched.
	// The site root, then the URL of the page to hand back (the file itself for
	// a single non-index page).
	siteURL := p.pagesURL(owner, repoName, dir)
	if autoURL != "" {
		siteURL = autoURL
	}
	if domain := readCNAME(ctx, client, owner, repoName, branch, dir); domain != "" {
		siteURL = "https://" + domain + "/"
	}
	urls := publishedURLs(siteURL, entries, dir)

	if t.DryRun {
		fmt.Fprintf(os.Stderr, "dry run — would publish %s to %s (branch %s%s):\n", entrySummary(entries, dir), p.repo, branch, dirNote(dir))
		for _, u := range urls {
			fmt.Fprintf(os.Stderr, "  → %s\n", u)
		}
		return provider.Result{URLs: urls}, nil
	}

	newCommit, err := pushCommit(ctx, client, owner, repoName, branch, publishMessage(entries), entries, t.Verbose)
	if err != nil {
		return provider.Result{}, err
	}
	if t.Verbose {
		fmt.Fprintf(os.Stderr, "commit: %s\n", newCommit.GetSHA())
	}

	// Best-effort for publish: the upload already succeeded, so a Pages-enable
	// hiccup is a warning, not a failure.
	if err := p.ensurePages(ctx, client, owner, repoName, branch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	// Friendly summary on stderr; the bare per-file URLs go to stdout for piping.
	fmt.Fprintf(os.Stderr, "✓ published %s to %s (branch %s%s)\n", entrySummary(entries, dir), p.repo, branch, dirNote(dir))
	return provider.Result{URLs: urls}, nil
}

func (p *Provider) ownerRepo() (string, string) {
	owner, name, _ := splitRepo(p.repo)
	return owner, name
}

func (p *Provider) pagesURL(owner, repo, dir string) string {
	var u string
	switch {
	case p.cname != "":
		u = "https://" + p.cname + "/"
	case repo == owner+".github.io":
		u = "https://" + owner + ".github.io/"
	default:
		u = "https://" + owner + ".github.io/" + repo + "/"
	}
	if dir != "" {
		u += encodeURLPath(dir) + "/"
	}
	return u
}

// ensurePages enables GitHub Pages (branch source, path /) unless it is already
// on. Only a 404 from GetPagesInfo means "not enabled yet"; any other error is
// surfaced rather than masked as "not enabled". When Pages is already on but
// serving a different source than what we just published to, it warns loudly —
// the upload would otherwise silently never appear. This is publish's path: it
// never repoints an existing config, since publish auto-detects the live source
// (autoTarget) and must not fight an intentional setup. The setup command, which
// is explicitly configuring the repo, uses reconcilePages to offer a repoint.
func (p *Provider) ensurePages(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	info, _, err := client.Repositories.GetPagesInfo(ctx, owner, repo)
	if err == nil {
		if w := pagesMismatchWarning(info.GetBuildType(), info.GetSource().GetBranch(), info.GetSource().GetPath(), branch); w != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		return nil // already enabled
	}
	if !is404(err) {
		return fmt.Errorf("checking GitHub Pages status: %w", err)
	}
	return enablePages(ctx, client, owner, repo, branch)
}

// pagesEnableAttempts is the total number of times enablePages POSTs before
// giving up (one initial try plus retries).
const pagesEnableAttempts = 4

// enablePages turns on GitHub Pages with a legacy (deploy-from-branch) source
// rooted at the branch's top level.
//
// The enable endpoint (POST /repos/.../pages) intermittently returns an opaque
// 500 — most often when called right after the source branch was just created,
// which is exactly what setup does. The write usually lands server-side despite
// the 500, so we retry with a short backoff and treat an "already enabled" 409
// (a prior 500 that actually took, or a concurrent enable) as success. Only 5xx
// is retried; any other error fails fast.
func enablePages(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	pages := &github.Pages{
		BuildType: github.Ptr("legacy"),
		Source: &github.PagesSource{
			Branch: github.Ptr(branch),
			Path:   github.Ptr("/"),
		},
	}

	var lastErr error
	for attempt := 1; attempt <= pagesEnableAttempts; attempt++ {
		if attempt > 1 {
			fmt.Fprintf(os.Stderr, "GitHub Pages enable returned a server error; retrying (%d/%d)...\n",
				attempt, pagesEnableAttempts)
			if err := sleep(ctx, pagesEnableBackoff(attempt)); err != nil {
				return err
			}
		}
		_, _, err := client.Repositories.EnablePages(ctx, owner, repo, pages)
		switch {
		case err == nil, isStatus(err, http.StatusConflict):
			return nil // created, or a prior attempt already enabled it
		case !isServerError(err):
			return fmt.Errorf("enabling GitHub Pages: %w", err) // not a transient 5xx
		default:
			lastErr = err
		}
	}
	return fmt.Errorf("enabling GitHub Pages: GitHub returned a server error %d times in a row.\n"+
		"This endpoint is intermittently flaky right after a branch is created; Pages may already be "+
		"enabled (check %s/settings/pages) — otherwise re-running setup usually succeeds: %w",
		pagesEnableAttempts, "https://github.com/"+owner+"/"+repo, lastErr)
}

// pagesEnableBackoff returns the wait before retry attempt n (2-based, since the
// first attempt has no wait): 1s, 2s, 3s — enough to outlast the brief window
// where a freshly created branch isn't yet visible to the Pages backend.
func pagesEnableBackoff(attempt int) time.Duration {
	return time.Duration(attempt-1) * time.Second
}

// sleep waits for d or until ctx is cancelled, whichever comes first.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// readCNAME returns the custom domain from a CNAME file at the target's source
// root (branch + dir), or "" if there is none. publish only reads it — writing
// the CNAME is `github setup`'s job.
func readCNAME(ctx context.Context, client *github.Client, owner, repo, branch, dir string) string {
	name := "CNAME"
	if dir != "" {
		name = path.Join(dir, "CNAME")
	}
	file, _, _, err := client.Repositories.GetContents(ctx, owner, repo, name, &github.RepositoryContentGetOptions{Ref: branch})
	if err != nil || file == nil {
		return ""
	}
	content, err := file.GetContent()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(content)
}

// autoTarget reports wherever GitHub Pages already serves from, so publish can
// target it instead of the flag defaults. ok is false (and the caller keeps its
// defaults) when Pages is off, built from a workflow, or otherwise can't be
// read. url is the live Pages URL, which may be empty even when ok. It reads
// only — resolving the target is the caller's job, so nothing on the provider
// is mutated.
func (p *Provider) autoTarget(ctx context.Context, client *github.Client, owner, repo string) (branch, dir, url string, ok bool) {
	info, _, err := client.Repositories.GetPagesInfo(ctx, owner, repo)
	if err != nil {
		return "", "", "", false
	}
	branch, dir, ok = pagesTarget(info.GetBuildType(), info.GetSource().GetBranch(), info.GetSource().GetPath())
	if !ok {
		return "", "", "", false
	}
	// GitHub returns html_url without a trailing slash for project sites (e.g.
	// https://owner.github.io/repo). servedURL builds links as base+path, so the
	// base must end in "/" or the per-file URLs come out glued (…/repostyle.css).
	return branch, dir, ensureSlash(info.GetHTMLURL()), true
}

// ensureSlash guarantees a non-empty URL ends with "/", so it is safe to use as
// a base that path segments are appended to.
func ensureSlash(u string) string {
	if u == "" || strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}

// pagesTarget maps a GitHub Pages branch source to a publish target. ok is false
// when there is no branch to push to (workflow build type or empty source), so
// the caller keeps its defaults.
func pagesTarget(buildType, srcBranch, srcPath string) (branch, dir string, ok bool) {
	if buildType == "workflow" || srcBranch == "" {
		return "", "", false
	}
	return srcBranch, strings.TrimPrefix(srcPath, "/"), true
}

// pagesMismatchWarning returns a message when GitHub Pages is configured to
// serve something other than the branch we just published to, or "" if it
// already matches. It does not change the user's Pages config — repointing
// could clobber an intentional setup — it just stops the silent no-show.
// Publish targets a branch (and dir), so the source path is not compared here;
// only a workflow build or a different branch counts as a mismatch.
func pagesMismatchWarning(buildType, srcBranch, srcPath, targetBranch string) string {
	if buildType != "workflow" && (srcBranch == "" || srcBranch == targetBranch) {
		return ""
	}
	return fmt.Sprintf("GitHub Pages is serving %s, not the %q branch this published to — "+
		"the upload will not appear until you repoint Pages (repo Settings → Pages)",
		pagesSourceDesc(buildType, srcBranch, srcPath), targetBranch)
}

// pagesSourceDesc renders a GitHub Pages source — a workflow build, or a branch
// at a path — as a human-readable phrase. The publish-time mismatch warning and
// the setup-time repoint prompt share it so the workflow-vs-branch wording lives
// in one place.
func pagesSourceDesc(buildType, srcBranch, srcPath string) string {
	if buildType == "workflow" {
		return "a GitHub Actions workflow"
	}
	return fmt.Sprintf("branch %s (path %s)", srcBranch, orSlash(srcPath))
}

// orSlash shows an empty Pages path as the root "/" it stands for.
func orSlash(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

// publishMessage builds the commit message for a publish, naming the file when
// it's a single one (the common case) and falling back to a count otherwise.
func publishMessage(entries []fileEntry) string {
	if len(entries) == 1 {
		return fmt.Sprintf("publish %s via htmlup", entries[0].path)
	}
	return fmt.Sprintf("publish %d files via htmlup", len(entries))
}

// servedPath maps an uploaded entry path to the path it's served at: the Pages
// source dir is the served root, so it's stripped.
func servedPath(p, dir string) string {
	if dir != "" {
		return strings.TrimPrefix(p, dir+"/")
	}
	return p
}

// publishedURLs returns the served URL of every published file, in upload
// order, so a multi-file publish hands back a usable link per file instead of a
// bare site root.
func publishedURLs(base string, entries []fileEntry, dir string) []string {
	urls := make([]string, len(entries))
	for i, e := range entries {
		urls[i] = servedURL(base, e.path, dir)
	}
	return urls
}

// servedURL is the URL an uploaded entry is served at. An index.html collapses
// to its directory root (where Pages serves it) so it reads as a clean link.
// base is expected to end with "/".
func servedURL(base, entryPath, dir string) string {
	sp := servedPath(entryPath, dir)
	switch {
	case sp == "index.html":
		return base
	case strings.HasSuffix(sp, "/index.html"):
		return base + encodeURLPath(strings.TrimSuffix(sp, "index.html"))
	default:
		return base + encodeURLPath(sp)
	}
}

func encodeURLPath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// entrySummary describes the published set for human-readable output.
func entrySummary(entries []fileEntry, dir string) string {
	if len(entries) == 1 {
		return servedPath(entries[0].path, dir)
	}
	return fmt.Sprintf("%d files", len(entries))
}

// dirNote renders the source subdirectory for human output (", /docs" or "").
func dirNote(dir string) string {
	if dir == "" {
		return ""
	}
	return ", /" + dir
}

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

// newGitHubClient builds a token-authenticated GitHub client. Auth is owned by
// go-github + oauth2 — see resolveToken for where the token comes from.
func newGitHubClient(ctx context.Context, token string) *github.Client {
	return github.NewClient(
		oauth2.NewClient(ctx, oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)),
	)
}

// httpStatus extracts the HTTP status code from a GitHub API error, reporting
// false when err is not a *github.ErrorResponse (e.g. a transport error).
func httpStatus(err error) (int, bool) {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		return ghErr.Response.StatusCode, true
	}
	return 0, false
}

// isStatus reports whether err is a GitHub API error carrying the given status.
func isStatus(err error, code int) bool {
	c, ok := httpStatus(err)
	return ok && c == code
}

// isServerError reports whether err is a GitHub API 5xx — a server-side fault
// that is worth retrying, unlike a 4xx that signals a bad request.
func isServerError(err error) bool {
	c, ok := httpStatus(err)
	return ok && c >= 500
}

// is404 reports whether err is a GitHub API "not found" response, the signal
// that a resource (a Pages config, a branch ref) does not exist yet.
func is404(err error) bool {
	return isStatus(err, http.StatusNotFound)
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
