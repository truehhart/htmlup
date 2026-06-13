package fsutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func walkNames(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	var names []string
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestResolveFS_Directory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "index.html"), []byte("<html>"))
	writeTestFile(t, filepath.Join(dir, "style.css"), []byte("body{}"))

	fsys, err := ResolveFS(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := walkNames(t, fsys)
	if len(names) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(names), names)
	}
}

func TestResolveFS_SingleFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "page.html")
	writeTestFile(t, fpath, []byte("<html>hello</html>"))

	fsys, err := ResolveFS(fpath)
	if err != nil {
		t.Fatal(err)
	}

	names := walkNames(t, fsys)
	if len(names) != 1 || names[0] != "page.html" {
		t.Fatalf("got %v, want [page.html]", names)
	}

	f, err := fsys.Open("page.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>hello</html>" {
		t.Errorf("content = %q, want '<html>hello</html>'", data)
	}
}

func TestResolveFS_NotFound(t *testing.T) {
	_, err := ResolveFS("/nonexistent/path/to/file")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}
