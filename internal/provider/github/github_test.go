package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-github/v72/github"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       *Provider
		wantErr bool
	}{
		{"valid", &Provider{repo: "owner/name", branch: "gh-pages"}, false},
		{"repo empty", &Provider{repo: "", branch: "gh-pages"}, true},
		{"repo no slash", &Provider{repo: "ownername", branch: "gh-pages"}, true},
		{"repo empty owner", &Provider{repo: "/name", branch: "gh-pages"}, true},
		{"repo empty name", &Provider{repo: "owner/", branch: "gh-pages"}, true},
		{"repo too many slashes", &Provider{repo: "a/b/c", branch: "gh-pages"}, false},
		{"branch empty", &Provider{repo: "owner/name"}, true},
		{"branch with slash", &Provider{repo: "owner/name", branch: "feature/demo"}, false},
		{"branch starts with slash", &Provider{repo: "owner/name", branch: "/demo"}, true},
		{"branch has parent segment", &Provider{repo: "owner/name", branch: "feature..demo"}, true},
		{"branch has lock suffix", &Provider{repo: "owner/name", branch: "feature/demo.lock"}, true},
		{"branch has reserved char", &Provider{repo: "owner/name", branch: "feature:demo"}, true},
		{"dir empty", &Provider{repo: "owner/name", branch: "gh-pages"}, false},
		{"dir clean nested", &Provider{repo: "owner/name", branch: "gh-pages", dir: "docs/reports"}, false},
		{"dir absolute", &Provider{repo: "owner/name", branch: "gh-pages", dir: "/docs"}, true},
		{"dir parent", &Provider{repo: "owner/name", branch: "gh-pages", dir: "../docs"}, true},
		{"dir not clean", &Provider{repo: "owner/name", branch: "gh-pages", dir: "docs/../site"}, true},
		{"dir backslash", &Provider{repo: "owner/name", branch: "gh-pages", dir: `docs\site`}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPagesURL(t *testing.T) {
	tests := []struct {
		name  string
		p     *Provider
		owner string
		repo  string
		want  string
	}{
		{
			"project repo",
			&Provider{branch: "gh-pages"},
			"owner", "myrepo",
			"https://owner.github.io/myrepo/",
		},
		{
			"user pages repo",
			&Provider{branch: "main"},
			"owner", "owner.github.io",
			"https://owner.github.io/",
		},
		{
			"user pages repo with dir",
			&Provider{branch: "main", dir: "docs"},
			"owner", "owner.github.io",
			"https://owner.github.io/docs/",
		},
		{
			"with dir",
			&Provider{branch: "gh-pages", dir: "docs"},
			"owner", "myrepo",
			"https://owner.github.io/myrepo/docs/",
		},
		{
			"with cname",
			&Provider{branch: "gh-pages", cname: "example.com"},
			"owner", "myrepo",
			"https://example.com/",
		},
		{
			"cname with dir",
			&Provider{branch: "gh-pages", cname: "example.com", dir: "v2"},
			"owner", "myrepo",
			"https://example.com/v2/",
		},
		{
			"dir is URL encoded",
			&Provider{branch: "gh-pages", dir: "release notes"},
			"owner", "myrepo",
			"https://owner.github.io/myrepo/release%20notes/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.pagesURL(tt.owner, tt.repo, tt.p.dir)
			if got != tt.want {
				t.Errorf("pagesURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectFiles(t *testing.T) {
	files := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>index</html>")},
		"css/style.css": &fstest.MapFile{Data: []byte("body{}")},
	}

	t.Run("no dir prefix", func(t *testing.T) {
		entries, err := collectFiles(files, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(entries))
		}
		if entries[0].path != "css/style.css" {
			t.Errorf("entries[0].path = %q, want css/style.css", entries[0].path)
		}
		if entries[1].path != "index.html" {
			t.Errorf("entries[1].path = %q, want index.html", entries[1].path)
		}
	})

	t.Run("with dir prefix", func(t *testing.T) {
		entries, err := collectFiles(files, "site")
		if err != nil {
			t.Fatal(err)
		}
		if entries[0].path != "site/css/style.css" {
			t.Errorf("entries[0].path = %q, want site/css/style.css", entries[0].path)
		}
		if entries[1].path != "site/index.html" {
			t.Errorf("entries[1].path = %q, want site/index.html", entries[1].path)
		}
	})

	t.Run("content read lazily", func(t *testing.T) {
		entries, err := collectFiles(files, "")
		if err != nil {
			t.Fatal(err)
		}
		data, err := entries[1].read()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "<html>index</html>" {
			t.Errorf("content = %q, want '<html>index</html>'", data)
		}
	})

	t.Run("empty fs", func(t *testing.T) {
		entries, err := collectFiles(fstest.MapFS{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("got %d entries, want 0", len(entries))
		}
	})
}

func TestHelloWorldHTML(t *testing.T) {
	html := helloWorldHTML(45, "truehhart/htmlup", "https://truehhart.github.io/htmlup/")

	if !strings.HasPrefix(strings.ToLower(html), "<!doctype html>") {
		t.Errorf("html should start with a doctype, got: %.20q", html)
	}
	if !strings.Contains(html, "htmlup") {
		t.Error("html should mention htmlup")
	}
	if !strings.Contains(html, "45 days") {
		t.Error("html should mention the interpolated TTL")
	}
	if !strings.Contains(html, "truehhart/htmlup") {
		t.Error("html should mention the interpolated repo")
	}
	if !strings.Contains(html, "https://truehhart.github.io/htmlup/") {
		t.Error("html should mention the interpolated Pages URL")
	}
	if strings.Contains(html, "{{") {
		t.Error("html still contains an uninterpolated placeholder")
	}
}

func TestPublishedURLs(t *testing.T) {
	const base = "https://truehhart.github.io/random-html-pages/"
	tests := []struct {
		name    string
		entries []fileEntry
		dir     string
		want    []string
	}{
		{
			"single non-index file links to the file",
			[]fileEntry{{path: "austin-powers-diagram.html"}},
			"",
			[]string{base + "austin-powers-diagram.html"},
		},
		{
			"single file under a source dir strips the dir",
			[]fileEntry{{path: "docs/austin-powers-diagram.html"}},
			"docs",
			[]string{base + "austin-powers-diagram.html"},
		},
		{
			"index.html links to the root",
			[]fileEntry{{path: "index.html"}},
			"",
			[]string{base},
		},
		{
			"directory with index.html lists the root then each asset",
			[]fileEntry{{path: "index.html"}, {path: "style.css"}},
			"",
			[]string{base, base + "style.css"},
		},
		{
			"multiple files without index each get their own URL",
			[]fileEntry{{path: "q3-report.html"}, {path: "security-audit.html"}},
			"",
			[]string{base + "q3-report.html", base + "security-audit.html"},
		},
		{
			"nested index.html collapses to its directory",
			[]fileEntry{{path: "reports/index.html"}},
			"",
			[]string{base + "reports/"},
		},
		{
			"reserved characters are URL encoded",
			[]fileEntry{{path: "reports/my report #1.html"}},
			"",
			[]string{base + "reports/my%20report%20%231.html"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := publishedURLs(base, tt.entries, tt.dir)
			if !slices.Equal(got, tt.want) {
				t.Errorf("publishedURLs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServedURL(t *testing.T) {
	const base = "https://owner.github.io/repo/"
	tests := []struct {
		name      string
		entryPath string
		dir       string
		want      string
	}{
		{"plain file", "style.css", "", base + "style.css"},
		{"index collapses to root", "index.html", "", base},
		{"nested index collapses to its dir", "reports/index.html", "", base + "reports/"},
		{"strips source dir", "docs/page.html", "docs", base + "page.html"},
		{"encodes reserved chars, keeps slashes", "reports/q3 #1.html", "", base + "reports/q3%20%231.html"},
		{"encodes nested index dir", "team docs/index.html", "", base + "team%20docs/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := servedURL(base, tt.entryPath, tt.dir); got != tt.want {
				t.Errorf("servedURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureSlash(t *testing.T) {
	// The auto-detect path feeds GitHub's html_url (no trailing slash for project
	// sites) into servedURL, whose base must end in "/".
	tests := []struct{ in, want string }{
		{"https://owner.github.io/repo", "https://owner.github.io/repo/"},
		{"https://owner.github.io/repo/", "https://owner.github.io/repo/"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ensureSlash(tt.in); got != tt.want {
			t.Errorf("ensureSlash(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPagesTarget(t *testing.T) {
	tests := []struct {
		name                          string
		buildType, srcBranch, srcPath string
		wantBranch, wantDir           string
		wantOK                        bool
	}{
		{"legacy docs folder", "legacy", "master", "/docs", "master", "docs", true},
		{"legacy root", "legacy", "gh-pages", "/", "gh-pages", "", true},
		{"workflow build type", "workflow", "", "", "", "", false},
		{"empty branch", "legacy", "", "/", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch, dir, ok := pagesTarget(tt.buildType, tt.srcBranch, tt.srcPath)
			if branch != tt.wantBranch || dir != tt.wantDir || ok != tt.wantOK {
				t.Errorf("pagesTarget() = (%q, %q, %v), want (%q, %q, %v)",
					branch, dir, ok, tt.wantBranch, tt.wantDir, tt.wantOK)
			}
		})
	}
}

func TestValidateSetup(t *testing.T) {
	base := func() *Provider {
		return &Provider{repo: "o/r", branch: "gh-pages", cron: "0 3 * * 0", ttlDays: 30}
	}
	tests := []struct {
		name    string
		mutate  func(*Provider)
		wantErr bool
	}{
		{"defaults ok", func(*Provider) {}, false},
		{"ttl zero rejected", func(p *Provider) { p.ttlDays = 0 }, true},
		{"ttl negative rejected", func(p *Provider) { p.ttlDays = -1 }, true},
		{"ttl one ok", func(p *Provider) { p.ttlDays = 1 }, false},
		{"cron with spaces ok", func(p *Provider) { p.cron = "*/5 * * * *" }, false},
		{"newline in branch rejected", func(p *Provider) { p.branch = "main\nfoo: bar" }, true},
		{"newline in cron rejected", func(p *Provider) { p.cron = "0 3 * * 0\nx" }, true},
		{"exclude with space rejected", func(p *Provider) { p.exclude = []string{"* secret"} }, true},
		{"plain exclude globs ok", func(p *Provider) { p.exclude = []string{"drafts", "*.keep"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base()
			tt.mutate(p)
			if err := p.validateSetup(); (err != nil) != tt.wantErr {
				t.Errorf("validateSetup() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestYAMLSingleQuoted(t *testing.T) {
	if got := yamlSingleQuoted("0 3 * * 0"); got != "0 3 * * 0" {
		t.Errorf("clean value changed: %q", got)
	}
	// An embedded quote must be doubled so it can't terminate the scalar.
	if got := yamlSingleQuoted("a'b"); got != "a''b" {
		t.Errorf("yamlSingleQuoted(a'b) = %q, want a''b", got)
	}
}

func TestHelloWorldHTMLEscapes(t *testing.T) {
	html := helloWorldHTML(30, "o/r<script>alert(1)</script>", "https://o.github.io/r")
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("repo value was interpolated as raw HTML — should be escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("expected the repo value to be HTML-escaped")
	}
}

func TestPublishMessage(t *testing.T) {
	one := publishMessage([]fileEntry{{path: "docs/index.html"}})
	if one != "publish docs/index.html via htmlup" {
		t.Errorf("single-file message = %q", one)
	}
	many := publishMessage([]fileEntry{{path: "a.html"}, {path: "b.html"}, {path: "c.html"}})
	if many != "publish 3 files via htmlup" {
		t.Errorf("multi-file message = %q", many)
	}
}

func TestPagesMismatchWarning(t *testing.T) {
	tests := []struct {
		name                          string
		buildType, srcBranch, srcPath string
		target                        string
		wantWarn                      bool
	}{
		{"match", "legacy", "gh-pages", "/", "gh-pages", false},
		{"different branch", "legacy", "master", "/docs", "gh-pages", true},
		{"workflow build type", "workflow", "", "", "gh-pages", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pagesMismatchWarning(tt.buildType, tt.srcBranch, tt.srcPath, tt.target)
			if (got != "") != tt.wantWarn {
				t.Errorf("pagesMismatchWarning() = %q, wantWarn %v", got, tt.wantWarn)
			}
		})
	}
}

func TestPagesRepointNeeded(t *testing.T) {
	tests := []struct {
		name                          string
		buildType, srcBranch, srcPath string
		target                        string
		want                          bool
	}{
		{"same branch root path", "legacy", "gh-pages", "/", "gh-pages", false},
		{"same branch empty path", "legacy", "gh-pages", "", "gh-pages", false},
		{"same branch docs path", "legacy", "gh-pages", "/docs", "gh-pages", true},
		{"different branch", "legacy", "gh-pages", "/", "master", true},
		{"workflow build type", "workflow", "", "", "gh-pages", true},
		{"empty source", "legacy", "", "/", "gh-pages", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pagesRepointNeeded(tt.buildType, tt.srcBranch, tt.srcPath, tt.target); got != tt.want {
				t.Errorf("pagesRepointNeeded(%q, %q, %q, %q) = %v, want %v", tt.buildType, tt.srcBranch, tt.srcPath, tt.target, got, tt.want)
			}
		})
	}
}

func TestPagesSourceDesc(t *testing.T) {
	tests := []struct {
		name                          string
		buildType, srcBranch, srcPath string
		want                          string
	}{
		{"branch with path", "legacy", "gh-pages", "/docs", "branch gh-pages (path /docs)"},
		{"branch empty path", "legacy", "gh-pages", "", "branch gh-pages (path /)"},
		{"workflow", "workflow", "", "", "a GitHub Actions workflow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pagesSourceDesc(tt.buildType, tt.srcBranch, tt.srcPath); got != tt.want {
				t.Errorf("pagesSourceDesc(%q, %q, %q) = %q, want %q", tt.buildType, tt.srcBranch, tt.srcPath, got, tt.want)
			}
		})
	}
}

func TestPagesRepointPrompt(t *testing.T) {
	// Branch mismatch: shows current source, the target, and a no-default prompt.
	branchPrompt := pagesRepointPrompt("owner/repo", "legacy", "gh-pages", "/docs", "master")
	for _, want := range []string{"owner/repo", "branch gh-pages (path /docs)", "branch master (path /)", "Repoint Pages to 'master'?", "[y/N]"} {
		if !strings.Contains(branchPrompt, want) {
			t.Errorf("branch prompt missing %q, got:\n%s", want, branchPrompt)
		}
	}

	// Workflow build type names the workflow rather than a phantom empty branch.
	wfPrompt := pagesRepointPrompt("owner/repo", "workflow", "", "", "gh-pages")
	if !strings.Contains(wfPrompt, "a GitHub Actions workflow") {
		t.Errorf("workflow prompt should name the workflow source, got:\n%s", wfPrompt)
	}
}

func TestCleanupWorkflowYAML(t *testing.T) {
	yaml := cleanupWorkflowYAML("0 5 * * 1", 14, "gh-pages", []string{"staging", "*.keep"})

	// All htmlup placeholders must be interpolated. (GitHub's own ${{ … }}
	// expressions legitimately remain, so we check our tokens specifically.)
	for _, ph := range []string{"{{CRON}}", "{{TTL_DAYS}}", "{{BRANCH}}", "{{EXCLUDE}}", "{{CLEANUP_SCRIPT}}"} {
		if strings.Contains(yaml, ph) {
			t.Errorf("yaml still contains uninterpolated placeholder %q", ph)
		}
	}

	wantContains := []string{
		`cron: '0 5 * * 1'`,             // cron interpolated
		`TTL_DAYS: '14'`,                // ttl interpolated into env
		`ref: 'gh-pages'`,               // branch interpolated
		`BRANCH: 'gh-pages'`,            // branch passed to the script via env
		"GH_TOKEN: ${{ github.token }}", // token for the signed API commit
		"workflow_dispatch:",            // manual trigger
		"contents: write",               // write permission
		"actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3", // SHA-pinned, per .github/CLAUDE.md
		"shell: bash -euo pipefail {0}",                                      // explicit hardened shell
		`name: '[Cleanup] | Remove entries older than TTL'`,                  // [TYPE] | Action naming
		"run-name: htmlup-cleanup (older than 14d)",                          // dynamic run-name
		"git -c core.quotepath=false ls-tree -z --name-only HEAD",            // inlined cleanup.sh body
		"git log -1 --format=%ct",
		"createCommitOnBranch", // signed commit via the GitHub API
	}
	for _, want := range wantContains {
		if !strings.Contains(yaml, want) {
			t.Errorf("yaml missing %q", want)
		}
	}

	// EXCLUDE_PATTERNS must hold the protected baseline plus user globs,
	// space-separated (cleanup.sh matches each as a glob).
	if !strings.Contains(yaml, "EXCLUDE_PATTERNS: 'index.html CNAME .nojekyll .github staging *.keep'") {
		t.Errorf("yaml should set EXCLUDE_PATTERNS to baseline + user globs, got:\n%s", yaml)
	}

	// The inlined script must be indented into the run: block (no stray shebang).
	if strings.Contains(yaml, "#!/usr/bin/env bash") {
		t.Error("inlined script should not carry its shebang into the run block")
	}
	if !strings.Contains(yaml, "\n          cutoff=$((") {
		t.Error("inlined script lines should be padded to the run: block column")
	}
}

func TestResolveToken(t *testing.T) {
	t.Run("from GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "gh-token-123")
		t.Setenv("GH_TOKEN", "")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "gh-token-123" {
			t.Errorf("got %q, want gh-token-123", token)
		}
	})

	t.Run("from GH_TOKEN", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "gh-token-456")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "gh-token-456" {
			t.Errorf("got %q, want gh-token-456", token)
		}
	})

	t.Run("GITHUB_TOKEN takes precedence", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "first")
		t.Setenv("GH_TOKEN", "second")

		token, err := resolveToken(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if token != "first" {
			t.Errorf("got %q, want first", token)
		}
	})

	t.Run("error when no token", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "")
		t.Setenv("PATH", t.TempDir())

		_, err := resolveToken(context.Background())
		if err == nil {
			t.Fatal("expected error when no token available")
		}
	})
}

// newTestClient returns a github.Client whose requests hit handler instead of
// the real API, so the multi-call git plumbing in pushCommit can be exercised
// offline.
func newTestClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	c := github.NewClient(nil)
	c.BaseURL = u
	return c
}

func TestPushCommitNewBranch(t *testing.T) {
	var blobs, refsCreated int
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			blobs++
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			refsCreated++
			_, _ = w.Write([]byte(`{"ref":"refs/heads/gh-pages"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	client := newTestClient(t, http.HandlerFunc(handler))

	entries := []fileEntry{
		{path: "index.html", read: staticContent([]byte("<html>"))},
		{path: "style.css", read: staticContent([]byte("body{}"))},
	}
	commit, err := pushCommit(context.Background(), client, "o", "r", "gh-pages", "msg", entries, false)
	if err != nil {
		t.Fatal(err)
	}
	if blobs != 2 {
		t.Errorf("created %d blobs, want 2 (one per entry)", blobs)
	}
	if refsCreated != 1 {
		t.Errorf("created %d refs, want 1 (missing branch should be created)", refsCreated)
	}
	if commit.GetSHA() != "newcommit" {
		t.Errorf("commit SHA = %q, want newcommit", commit.GetSHA())
	}
}

func TestPushCommitExistingBranch(t *testing.T) {
	var refsUpdated, refsCreated int
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			_, _ = w.Write([]byte(`{"ref":"refs/heads/gh-pages","object":{"sha":"basecommit"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"sha":"basecommit","tree":{"sha":"basetree"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			// New tree SHA differs from the base tree, so the commit proceeds.
			_, _ = w.Write([]byte(`{"sha":"newtree"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			refsUpdated++
			_, _ = w.Write([]byte(`{"ref":"refs/heads/gh-pages","object":{"sha":"newcommit"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			refsCreated++
			_, _ = w.Write([]byte(`{"ref":"refs/heads/gh-pages"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	client := newTestClient(t, http.HandlerFunc(handler))

	commit, err := pushCommit(context.Background(), client, "o", "r", "gh-pages", "msg",
		[]fileEntry{{path: "index.html", read: staticContent([]byte("<html>"))}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if refsUpdated != 1 {
		t.Errorf("updated %d refs, want 1 (existing branch ref should be moved)", refsUpdated)
	}
	if refsCreated != 0 {
		t.Errorf("created %d refs, want 0 (branch already exists)", refsCreated)
	}
	if commit.GetSHA() != "newcommit" {
		t.Errorf("commit SHA = %q, want newcommit", commit.GetSHA())
	}
}

// TestPushCommitNoChange covers the idempotent path: when the merged tree
// matches the branch's current tree, no commit is created and the branch ref is
// left untouched (so Pages doesn't needlessly rebuild).
func TestPushCommitNoChange(t *testing.T) {
	var commitsCreated, refWrites int
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			_, _ = w.Write([]byte(`{"ref":"refs/heads/gh-pages","object":{"sha":"basecommit"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"sha":"basecommit","tree":{"sha":"basetree"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			// Same SHA as the base tree → nothing changed.
			_, _ = w.Write([]byte(`{"sha":"basetree"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			commitsCreated++
			_, _ = w.Write([]byte(`{"sha":"newcommit"}`))
		case strings.Contains(r.URL.Path, "/git/refs"):
			refWrites++
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	client := newTestClient(t, http.HandlerFunc(handler))

	commit, err := pushCommit(context.Background(), client, "o", "r", "gh-pages", "msg",
		[]fileEntry{{path: "index.html", read: staticContent([]byte("<html>"))}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if commitsCreated != 0 {
		t.Errorf("created %d commits, want 0 (tree unchanged)", commitsCreated)
	}
	if refWrites != 0 {
		t.Errorf("wrote ref %d times, want 0 (tree unchanged)", refWrites)
	}
	if commit.GetSHA() != "basecommit" {
		t.Errorf("commit SHA = %q, want basecommit (the existing HEAD)", commit.GetSHA())
	}
}

// TestCleanupScript runs the embedded cleanup.sh against a throwaway git repo so
// the workflow's actual entry-selection logic is exercised, not just its
// rendering. It runs in DRY_RUN mode — selecting + staging the stale entries is
// what's worth testing; the final commit goes through the GitHub API and can't
// be exercised offline. Portable (no GNU-only date parsing), so it runs on both
// CI and macOS dev machines.
func TestCleanupScript(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	repo := t.TempDir()
	// Keep the script outside the repo so it isn't itself a tracked entry.
	script := filepath.Join(t.TempDir(), "cleanup.sh")
	if err := os.WriteFile(script, []byte(cleanupScript), 0o755); err != nil {
		t.Fatal(err)
	}

	const old = "2000-01-01T00:00:00Z" // well past any TTL

	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(repo, name))
		return err == nil
	}

	git(nil, "init", "--quiet")
	git(nil, "config", "user.email", "test@example.com")
	git(nil, "config", "user.name", "test")

	// Old entries (committed in the past): some protected, some not. "café.html"
	// is non-ASCII on purpose — git C-quotes such names by default, which the
	// loop must decode or it would silently never clean them up.
	for _, f := range []string{"index.html", "stale.html", "café.html", "keep.html", "notes.keep", "staging/x.html", "archive/y.html"} {
		write(f, "old")
	}
	git(nil, "add", "-A")
	oldDate := []string{"GIT_AUTHOR_DATE=" + old, "GIT_COMMITTER_DATE=" + old}
	git(oldDate, "commit", "--quiet", "-m", "old content")

	// A fresh entry committed now — must survive the TTL.
	write("fresh.html", "new")
	git(nil, "add", "-A")
	git(nil, "commit", "--quiet", "-m", "fresh content")

	cmd := exec.Command("bash", script)
	cmd.Dir = repo
	cmd.Env = append(
		os.Environ(),
		"DRY_RUN=1",
		"TTL_DAYS=1",
		"BRANCH=main",
		"EXCLUDE_PATTERNS=index.html CNAME .nojekyll .github keep.html *.keep staging",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cleanup.sh failed: %v\n%s", err, out)
	}

	// Stale, non-excluded entries are staged for deletion (gone from the tree);
	// everything else survives.
	for _, gone := range []string{"stale.html", "café.html", "archive"} {
		if exists(gone) {
			t.Errorf("expected %q to be deleted", gone)
		}
	}
	for _, kept := range []string{"index.html", "keep.html", "notes.keep", "staging", "fresh.html"} {
		if !exists(kept) {
			t.Errorf("expected %q to be kept", kept)
		}
	}
}

func TestHTTPStatusClassification(t *testing.T) {
	ghErr := func(code int) error {
		return &github.ErrorResponse{Response: &http.Response{StatusCode: code}}
	}
	tests := []struct {
		name                   string
		err                    error
		is404, isServer, is409 bool
	}{
		{"nil response", &github.ErrorResponse{}, false, false, false},
		{"transport error", context.DeadlineExceeded, false, false, false},
		{"404", ghErr(http.StatusNotFound), true, false, false},
		{"409", ghErr(http.StatusConflict), false, false, true},
		{"403", ghErr(http.StatusForbidden), false, false, false},
		{"500", ghErr(http.StatusInternalServerError), false, true, false},
		{"503", ghErr(http.StatusServiceUnavailable), false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := is404(tc.err); got != tc.is404 {
				t.Errorf("is404 = %v, want %v", got, tc.is404)
			}
			if got := isServerError(tc.err); got != tc.isServer {
				t.Errorf("isServerError = %v, want %v", got, tc.isServer)
			}
			if got := isStatus(tc.err, http.StatusConflict); got != tc.is409 {
				t.Errorf("isStatus(409) = %v, want %v", got, tc.is409)
			}
		})
	}
}

func TestPagesEnableBackoff(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: 0, 2: time.Second, 3: 2 * time.Second} {
		if got := pagesEnableBackoff(attempt); got != want {
			t.Errorf("pagesEnableBackoff(%d) = %v, want %v", attempt, got, want)
		}
	}
}

// TestEnablePagesRetriesServerError covers the exact case that motivated the
// retry: the enable endpoint 500s on the first POST (a freshly created branch
// not yet visible to the Pages backend) but the write lands, so the retried
// POST sees it as already enabled (409). enablePages must treat that as success.
func TestEnablePagesRetriesServerError(t *testing.T) {
	var posts int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/pages") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		posts++
		if posts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"GitHub Pages is already enabled."}`))
	}))

	if err := enablePages(context.Background(), client, "o", "r", "gh-pages"); err != nil {
		t.Fatalf("enablePages = %v, want nil (transient 500 then 409 should succeed)", err)
	}
	if posts != 2 {
		t.Errorf("made %d POSTs, want 2 (one 500, one 409)", posts)
	}
}

// TestEnablePagesFailsFastOn4xx verifies a non-5xx error is returned immediately
// without burning the retry budget — a 403 is a real problem, not flakiness.
func TestEnablePagesFailsFastOn4xx(t *testing.T) {
	var posts int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}))

	err := enablePages(context.Background(), client, "o", "r", "gh-pages")
	if err == nil {
		t.Fatal("enablePages = nil, want error on 403")
	}
	if posts != 1 {
		t.Errorf("made %d POSTs, want 1 (4xx must not be retried)", posts)
	}
}
