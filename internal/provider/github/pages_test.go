package github

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPagesURL(t *testing.T) {
	tests := []struct {
		name  string
		p     *Provider
		owner string
		repo  string
		want  string
	}{
		{"project repo", &Provider{branch: "gh-pages"}, "owner", "myrepo", "https://owner.github.io/myrepo/"},
		{"user pages repo", &Provider{branch: "main"}, "owner", "owner.github.io", "https://owner.github.io/"},
		{"user pages repo with dir", &Provider{branch: "main", dir: "docs"}, "owner", "owner.github.io", "https://owner.github.io/docs/"},
		{"with dir", &Provider{branch: "gh-pages", dir: "docs"}, "owner", "myrepo", "https://owner.github.io/myrepo/docs/"},
		{"with cname", &Provider{branch: "gh-pages", cname: "example.com"}, "owner", "myrepo", "https://example.com/"},
		{"cname with dir", &Provider{branch: "gh-pages", cname: "example.com", dir: "v2"}, "owner", "myrepo", "https://example.com/v2/"},
		{"dir is URL encoded", &Provider{branch: "gh-pages", dir: "release notes"}, "owner", "myrepo", "https://owner.github.io/myrepo/release%20notes/"},
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

func TestReadCNAME(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{
			name:   "valid domain",
			status: http.StatusOK,
			body:   `{"type":"file","encoding":"base64","content":"ZXhhbXBsZS5jb20K"}`,
			want:   "example.com",
		},
		{
			name:   "missing CNAME",
			status: http.StatusNotFound,
			body:   `{"message":"Not Found"}`,
		},
		{
			name:    "server error",
			status:  http.StatusInternalServerError,
			body:    `{"message":"temporary failure"}`,
			wantErr: true,
		},
		{
			name:    "malformed domain",
			status:  http.StatusOK,
			body:    `{"type":"file","encoding":"base64","content":"ZXhhbXBsZS5jb20gYmFkCg=="}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/contents/CNAME") {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			got, err := readCNAME(context.Background(), client, "o", "r", "gh-pages", "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("readCNAME() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("readCNAME() = %q, want %q", got, tt.want)
			}
		})
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
		{"single non-index file links to the file", []fileEntry{{path: "austin-powers-diagram.html"}}, "", []string{base + "austin-powers-diagram.html"}},
		{"single file under a source dir strips the dir", []fileEntry{{path: "docs/austin-powers-diagram.html"}}, "docs", []string{base + "austin-powers-diagram.html"}},
		{"index.html links to the root", []fileEntry{{path: "index.html"}}, "", []string{base}},
		{"directory with index.html lists the root then each asset", []fileEntry{{path: "index.html"}, {path: "style.css"}}, "", []string{base, base + "style.css"}},
		{"multiple files without index each get their own URL", []fileEntry{{path: "q3-report.html"}, {path: "security-audit.html"}}, "", []string{base + "q3-report.html", base + "security-audit.html"}},
		{"nested index.html collapses to its directory", []fileEntry{{path: "reports/index.html"}}, "", []string{base + "reports/"}},
		{"reserved characters are URL encoded", []fileEntry{{path: "reports/my report #1.html"}}, "", []string{base + "reports/my%20report%20%231.html"}},
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
	branchPrompt := pagesRepointPrompt("owner/repo", "legacy", "gh-pages", "/docs", "master")
	for _, want := range []string{"owner/repo", "branch gh-pages (path /docs)", "branch master (path /)", "Repoint Pages to 'master'?", "[y/N]"} {
		if !strings.Contains(branchPrompt, want) {
			t.Errorf("branch prompt missing %q, got:\n%s", want, branchPrompt)
		}
	}

	wfPrompt := pagesRepointPrompt("owner/repo", "workflow", "", "", "gh-pages")
	if !strings.Contains(wfPrompt, "a GitHub Actions workflow") {
		t.Errorf("workflow prompt should name the workflow source, got:\n%s", wfPrompt)
	}
}

func TestPagesEnableBackoff(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: 0, 2: time.Second, 3: 2 * time.Second} {
		if got := pagesEnableBackoff(attempt); got != want {
			t.Errorf("pagesEnableBackoff(%d) = %v, want %v", attempt, got, want)
		}
	}
}

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
