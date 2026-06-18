package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestCleanupWorkflowYAML(t *testing.T) {
	yaml := cleanupWorkflowYAML("0 5 * * 1", 14, "gh-pages", []string{"staging", "*.keep"})

	for _, ph := range []string{"{{CRON}}", "{{TTL_DAYS}}", "{{BRANCH}}", "{{EXCLUDE}}", "{{CLEANUP_SCRIPT}}"} {
		if strings.Contains(yaml, ph) {
			t.Errorf("yaml still contains uninterpolated placeholder %q", ph)
		}
	}

	wantContains := []string{
		`cron: '0 5 * * 1'`,
		`TTL_DAYS: '14'`,
		`ref: 'gh-pages'`,
		`BRANCH: 'gh-pages'`,
		"GH_TOKEN: ${{ github.token }}",
		"workflow_dispatch:",
		"contents: write",
		"actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3",
		"shell: bash -euo pipefail {0}",
		`name: '[Cleanup] | Remove entries older than TTL'`,
		"run-name: htmlup-cleanup (older than 14d)",
		"git -c core.quotepath=false ls-tree -z --name-only HEAD",
		"git log -1 --format=%ct",
		"createCommitOnBranch",
	}
	for _, want := range wantContains {
		if !strings.Contains(yaml, want) {
			t.Errorf("yaml missing %q", want)
		}
	}

	if !strings.Contains(yaml, "EXCLUDE_PATTERNS: 'index.html CNAME .nojekyll .github staging *.keep'") {
		t.Errorf("yaml should set EXCLUDE_PATTERNS to baseline + user globs, got:\n%s", yaml)
	}
	if strings.Contains(yaml, "#!/usr/bin/env bash") {
		t.Error("inlined script should not carry its shebang into the run block")
	}
	if !strings.Contains(yaml, "\n          cutoff=$((") {
		t.Error("inlined script lines should be padded to the run: block column")
	}
}

func TestCleanupScript(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	repo := t.TempDir()
	script := filepath.Join(t.TempDir(), "cleanup.sh")
	if err := os.WriteFile(script, []byte(cleanupScript), 0o755); err != nil {
		t.Fatal(err)
	}

	const old = "2000-01-01T00:00:00Z"

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

	for _, f := range []string{"index.html", "stale.html", "café.html", "keep.html", "notes.keep", "staging/x.html", "archive/y.html"} {
		write(f, "old")
	}
	git(nil, "add", "-A")
	oldDate := []string{"GIT_AUTHOR_DATE=" + old, "GIT_COMMITTER_DATE=" + old}
	git(oldDate, "commit", "--quiet", "-m", "old content")

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
