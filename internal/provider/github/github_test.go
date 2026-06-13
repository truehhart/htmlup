package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{"valid", "owner/name", false},
		{"empty", "", true},
		{"no slash", "ownername", true},
		{"empty owner", "/name", true},
		{"empty name", "owner/", true},
		{"too many slashes", "a/b/c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{repo: tt.repo}
			err := p.validate()
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.pagesURL(tt.owner, tt.repo)
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

	t.Run("content preserved", func(t *testing.T) {
		entries, err := collectFiles(files, "")
		if err != nil {
			t.Fatal(err)
		}
		if string(entries[1].content) != "<html>index</html>" {
			t.Errorf("content = %q, want '<html>index</html>'", entries[1].content)
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
		"git ls-tree --name-only HEAD",                                       // inlined cleanup.sh body
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

	// Old entries (committed in the past): some protected, some not.
	for _, f := range []string{"index.html", "stale.html", "keep.html", "notes.keep", "staging/x.html", "archive/y.html"} {
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
	for _, gone := range []string{"stale.html", "archive"} {
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
