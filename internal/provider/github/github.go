package github

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/spf13/cobra"

	htmlconfig "github.com/truehhart/htmlup/internal/config"
	"github.com/truehhart/htmlup/internal/provider"
	"github.com/truehhart/htmlup/internal/ui"
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

func (p *Provider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:      "repo",
			Label:    "Repository (owner/name)",
			Help:     "Target repository that hosts the GitHub Pages site.",
			Required: true,
			Validate: func(v string) error {
				if _, _, ok := splitRepo(v); !ok {
					return fmt.Errorf("must be in owner/name format")
				}
				return nil
			},
		},
		// branch and dir aren't config-init fields. The profile only needs the
		// repo; at publish time the provider auto-detects whichever branch &
		// directory GitHub Pages already serves from. Initial Pages setup
		// (picking the branch) is `htmlup github setup`'s job, not init's.
	}
}

func (p *Provider) PublishCommand() *cobra.Command {
	var profileName string
	cmd := &cobra.Command{
		Use:   "github <path>",
		Short: "Publish HTML to GitHub Pages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flags parsed cleanly; from here errors are runtime failures, not
			// misuse, so don't tack the usage screen onto them.
			cmd.SilenceUsage = true
			out := ui.Auto()
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			verbose, _ := cmd.Flags().GetBool("verbose")
			profile, _, err := provider.SelectedProfile(cmd, p.Name(), profileName)
			if err != nil {
				return err
			}
			applied := p.applyProfile(profile, cmd)
			if err := p.validate(); err != nil {
				return err
			}
			files, err := provider.PrepareFiles(cmd, args[0], out)
			if err != nil {
				return err
			}
			// By default, target wherever Pages already serves from. Setting
			// --branch/--dir explicitly (or --no-auto) opts back into manual mode.
			autoDetect := !p.noAuto && !applied.manualTarget && !cmd.Flags().Changed("branch") && !cmd.Flags().Changed("dir")
			result, err := p.publish(cmd.Context(), provider.Target{
				Files:   files,
				DryRun:  dryRun,
				Verbose: verbose,
				UI:      out,
			}, autoDetect)
			if err != nil {
				return err
			}
			out.Result(result.URLs...)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.repo, "repo", "", "target repository (owner/name)")
	cmd.Flags().StringVar(&p.branch, "branch", "gh-pages", "branch to push to (default: auto-detected from Pages settings)")
	cmd.Flags().StringVar(&p.dir, "dir", "", "subdirectory within the branch (default: auto-detected from Pages settings)")
	cmd.Flags().BoolVar(&p.noAuto, "no-auto", false, "don't auto-detect the target from GitHub Pages settings; use --branch/--dir as given")
	cmd.Flags().StringVar(&profileName, "profile", "", "config profile name to use for github")
	return cmd
}

func (p *Provider) Publish(ctx context.Context, files fs.FS, profile htmlconfig.Profile, dryRun, verbose bool, out *ui.Output) (provider.Result, error) {
	applied := p.applyProfile(profile, nil)
	if err := p.validate(); err != nil {
		return provider.Result{}, err
	}
	autoDetect := !p.noAuto && !applied.manualTarget
	return p.publish(ctx, provider.Target{
		Files:   files,
		DryRun:  dryRun,
		Verbose: verbose,
		UI:      out,
	}, autoDetect)
}

type profileApplyResult struct {
	manualTarget bool
}

func (p *Provider) applyProfile(profile htmlconfig.Profile, cmd *cobra.Command) profileApplyResult {
	var result profileApplyResult
	if profile == nil {
		return result
	}
	if v := profile["repo"]; v != "" && !provider.FlagChanged(cmd, "repo") {
		p.repo = v
	}
	if v := profile["branch"]; v != "" && !provider.FlagChanged(cmd, "branch") {
		p.branch = v
		result.manualTarget = true
	}
	if v, ok := profile["dir"]; ok && !provider.FlagChanged(cmd, "dir") {
		p.dir = v
		result.manualTarget = true
	}
	if v := profile["no_auto"]; v != "" && !provider.FlagChanged(cmd, "no-auto") {
		p.noAuto = v == "true"
	}
	return result
}

func (p *Provider) validate() error {
	if _, _, ok := splitRepo(p.repo); !ok {
		return fmt.Errorf("--repo must be in owner/name format (set it with --repo or a config profile)")
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
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
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
				t.UI.Progress("auto-detected Pages source: branch %s, dir %q", branch, dir)
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
	domain, err := readCNAME(ctx, client, owner, repoName, branch, dir)
	if err != nil {
		return provider.Result{}, fmt.Errorf("reading CNAME: %w", err)
	}
	if domain != "" {
		siteURL = "https://" + domain + "/"
	}
	urls := publishedURLs(siteURL, entries, dir)

	if t.DryRun {
		t.UI.DryRun("would publish %s to %s (branch %s%s)", entrySummary(entries, dir), p.repo, branch, dirNote(dir))
		for _, u := range urls {
			t.UI.Detail("%s", u)
		}
		return provider.Result{URLs: urls}, nil
	}

	newCommit, err := pushCommit(ctx, client, owner, repoName, branch, publishMessage(entries), entries, t.Verbose, t.UI)
	if err != nil {
		return provider.Result{}, err
	}
	if t.Verbose {
		t.UI.Progress("commit %s", newCommit.GetSHA())
	}

	// Best-effort for publish: the upload already succeeded, so a Pages-enable
	// hiccup is a warning, not a failure.
	if err := p.ensurePages(ctx, client, owner, repoName, branch, t.UI); err != nil {
		t.UI.Warn("%v", err)
	}

	// Human summary on stderr; the bare per-file URLs go to stdout for piping.
	t.UI.Success("published %s to %s (branch %s%s)", entrySummary(entries, dir), p.repo, branch, dirNote(dir))
	return provider.Result{URLs: urls}, nil
}

func (p *Provider) ownerRepo() (string, string) {
	owner, name, _ := splitRepo(p.repo)
	return owner, name
}
