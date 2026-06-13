package github

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v72/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/truehhart/htmlup/internal/provider"
)

// cleanupWorkflowPath is where the cron cleanup workflow is committed in the
// target repo's default branch.
const cleanupWorkflowPath = ".github/workflows/htmlup-cleanup.yaml"

func (p *Provider) setupCmd() *cobra.Command {
	var (
		dryRun  bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap a GitHub Pages repo for use with htmlup",
		Long: "Enable GitHub Pages on the target repo, publish a hello-world landing page " +
			"to the Pages branch, and install an opt-in cron cleanup workflow on the default branch.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := p.validate(); err != nil {
				return err
			}
			result, err := p.setup(cmd.Context(), dryRun, verbose)
			if err != nil {
				return err
			}
			fmt.Println(result.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.repo, "repo", "", "target repository (owner/name)")
	_ = cmd.MarkFlagRequired("repo")
	cmd.Flags().StringVar(&p.branch, "branch", "gh-pages", "Pages branch to bootstrap")
	cmd.Flags().IntVar(&p.ttlDays, "ttl-days", 30, "delete published files older than this many days")
	cmd.Flags().StringVar(&p.cron, "cron", "0 3 * * 0", "cron schedule for the cleanup workflow")
	cmd.Flags().StringSliceVar(&p.exclude, "exclude", nil, "additional glob patterns the cleanup never deletes (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be done without writing")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "per-step progress and SDK detail")
	return cmd
}

func (p *Provider) setup(ctx context.Context, dryRun, verbose bool) (provider.Result, error) {
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

	url := p.pagesURL(owner, repoName)
	landing := helloWorldHTML(p.ttlDays, p.repo, url)
	workflow := cleanupWorkflowYAML(p.cron, p.ttlDays, p.branch, p.exclude)

	if dryRun {
		fmt.Fprintf(os.Stderr, "would publish landing page index.html to %s branch %s\n", p.repo, p.branch)
		fmt.Fprintf(os.Stderr, "would enable GitHub Pages (branch %s, path /)\n", p.branch)
		fmt.Fprintf(os.Stderr, "would install %s to the default branch (cron %q, ttl %d days)\n", cleanupWorkflowPath, p.cron, p.ttlDays)
		return provider.Result{URL: url}, nil
	}

	// 1. Publish the hello-world landing page to the Pages branch.
	landingCommit, err := pushCommit(ctx, client, owner, repoName, p.branch,
		"bootstrap landing page via htmlup",
		[]fileEntry{{path: "index.html", content: []byte(landing)}}, verbose)
	if err != nil {
		return provider.Result{}, err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "landing commit: %s\n", landingCommit.GetSHA())
	}

	// 2. Enable Pages on the bootstrapped branch.
	p.ensurePages(ctx, client, owner, repoName)

	// 3. Install the cron cleanup workflow on the repo's default branch.
	defaultBranch, err := p.defaultBranch(ctx, client, owner, repoName)
	if err != nil {
		return provider.Result{}, err
	}
	workflowCommit, err := pushCommit(ctx, client, owner, repoName, defaultBranch,
		"install htmlup cleanup workflow via htmlup",
		[]fileEntry{{path: cleanupWorkflowPath, content: []byte(workflow)}}, verbose)
	if err != nil {
		return provider.Result{}, err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "workflow commit: %s (branch %s)\n", workflowCommit.GetSHA(), defaultBranch)
		fmt.Fprintf(os.Stderr, "bootstrapped %s -> %s\n", p.repo, url)
	}

	return provider.Result{URL: url}, nil
}

func (p *Provider) defaultBranch(ctx context.Context, client *github.Client, owner, repo string) (string, error) {
	r, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("getting repository %s: %w", p.repo, err)
	}
	return r.GetDefaultBranch(), nil
}

// helloWorldTemplate / cleanupWorkflowTemplate are the real HTML and YAML files
// under templates/, embedded at build time and interpolated below. Editing them
// as standalone files keeps them lintable (htmlhint / check-yaml) and gives
// editors proper syntax support.

//go:embed templates/hello-world.html
var helloWorldTemplate string

//go:embed templates/cleanup-workflow.yaml
var cleanupWorkflowTemplate string

// cleanupScript is the portable cleanup logic. It is unit-tested directly
// (TestCleanupScript) and inlined into the workflow's run: block at generation
// time, so the workflow stays self-contained while the logic stays testable.
//
//go:embed templates/cleanup.sh
var cleanupScript string

// helloWorldHTML returns a self-contained HTML5 landing page noting that the
// repo was bootstrapped by htmlup and that stale uploads auto-expire after the
// given TTL in days. The demo terminal is filled with the target repo (owner/name)
// and its Pages URL so the page reflects the repo it was published to.
func helloWorldHTML(ttlDays int, repo, url string) string {
	r := strings.NewReplacer(
		"{{TTL_DAYS}}", strconv.Itoa(ttlDays),
		"{{REPO}}", repo,
		"{{URL}}", url,
	)
	return r.Replace(helloWorldTemplate)
}

// cleanupWorkflowYAML returns the cron cleanup GitHub Actions workflow,
// parameterized by the cron schedule, the TTL in days, the Pages branch, and
// any extra exclude globs the cleanup must never delete. The cleanup logic
// itself is inlined from cleanup.sh.
func cleanupWorkflowYAML(cron string, ttlDays int, branch string, exclude []string) string {
	r := strings.NewReplacer(
		"{{CRON}}", cron,
		"{{TTL_DAYS}}", strconv.Itoa(ttlDays),
		"{{BRANCH}}", branch,
		"{{EXCLUDE}}", cleanupExcludePattern(exclude),
		"{{CLEANUP_SCRIPT}}", indentScript(cleanupScript, scriptIndent(cleanupWorkflowTemplate)),
	)
	return r.Replace(cleanupWorkflowTemplate)
}

// cleanupExcludePattern builds the space-separated glob list (EXCLUDE_PATTERNS)
// of entries the cleanup must never delete: the always-protected baseline plus
// any user globs. cleanup.sh matches each glob against top-level entries.
func cleanupExcludePattern(extra []string) string {
	patterns := []string{"index.html", "CNAME", ".nojekyll", ".github"}
	for _, e := range extra {
		if e = strings.TrimSpace(e); e != "" {
			patterns = append(patterns, e)
		}
	}
	return strings.Join(patterns, " ")
}

// scriptIndent returns the leading-space width of the line holding the
// {{CLEANUP_SCRIPT}} placeholder, so the inlined script matches the template's
// run: block column without a hardcoded constant that could drift from the YAML.
func scriptIndent(tmpl string) int {
	for _, line := range strings.Split(tmpl, "\n") {
		if strings.Contains(line, "{{CLEANUP_SCRIPT}}") {
			return len(line) - len(strings.TrimLeft(line, " "))
		}
	}
	return 0
}

// indentScript inlines cleanup.sh into a YAML run: block: the shebang is dropped
// (the step already sets `shell: bash …`) and every non-empty line after the
// first is padded to the block's column. The first line keeps the indentation
// already present before the {{CLEANUP_SCRIPT}} placeholder.
func indentScript(script string, indent int) string {
	body := strings.TrimPrefix(script, "#!/usr/bin/env bash\n")
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, l := range lines {
		if i > 0 && l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}
