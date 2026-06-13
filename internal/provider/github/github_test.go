package github

import (
	"context"
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

func TestCleanupWorkflowYAML(t *testing.T) {
	yaml := cleanupWorkflowYAML("0 5 * * 1", 14, "gh-pages", []string{"drafts/*", "keep.html"})

	if strings.Contains(yaml, "{{") {
		t.Error("yaml still contains an uninterpolated placeholder")
	}

	wantContains := []string{
		`cron: '0 5 * * 1'`,  // cron interpolated
		`TTL_DAYS: '14'`,     // ttl interpolated
		`ref: 'gh-pages'`,    // branch interpolated
		"workflow_dispatch:", // manual trigger
		"permissions:",       // permissions block
		"contents: write",    // write permission
		"actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3", // SHA-pinned, per .github/CLAUDE.md
		"shell: bash -euo pipefail {0}",                                      // explicit hardened shell
		`name: '[Setup] | Checkout Pages branch'`,                            // [TYPE] | Action naming
		`name: '[Cleanup] | Delete entries older than TTL'`,
		"git log -1 --format=%cI",
	}
	for _, want := range wantContains {
		if !strings.Contains(yaml, want) {
			t.Errorf("yaml missing %q", want)
		}
	}

	// The exclude case clause must hold the protected baseline plus user globs.
	wantExcluded := []string{"index.html", "CNAME", ".nojekyll", ".github", "drafts/*", "keep.html"}
	for _, e := range wantExcluded {
		if !strings.Contains(yaml, e) {
			t.Errorf("yaml exclude clause should reference %q", e)
		}
	}
	if !strings.Contains(yaml, "index.html|CNAME|.nojekyll|.github|drafts/*|keep.html)") {
		t.Error("yaml should join baseline + user excludes into one case clause")
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
