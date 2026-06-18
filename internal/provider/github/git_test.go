package github

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

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
