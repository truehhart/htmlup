package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v72/github"
)

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
// the CNAME is `github setup`'s job. A missing CNAME is fine; other API/content
// errors are surfaced so publish does not print a URL it could not verify.
func readCNAME(ctx context.Context, client *github.Client, owner, repo, branch, dir string) (string, error) {
	name := "CNAME"
	if dir != "" {
		name = path.Join(dir, "CNAME")
	}
	file, _, _, err := client.Repositories.GetContents(ctx, owner, repo, name, &github.RepositoryContentGetOptions{Ref: branch})
	if is404(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if file == nil {
		return "", nil
	}
	content, err := file.GetContent()
	if err != nil {
		return "", err
	}
	domain := strings.TrimSpace(content)
	if domain == "" {
		return "", nil
	}
	if strings.ContainsAny(domain, " \t\n\r") {
		return "", fmt.Errorf("invalid CNAME content %q", domain)
	}
	return domain, nil
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
// only a workflow build or a different branch counts as a mismatch. Setup is
// stricter because it bootstraps the landing page at the branch root, so its
// pagesRepointNeeded predicate must also reject "/docs".
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
	return (&url.URL{Path: p}).EscapedPath()
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
