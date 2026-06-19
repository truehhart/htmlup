package htmlcrypt

import (
	"bytes"
	"encoding/base64"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSealOpenRoundTrip(t *testing.T) {
	plain := []byte("<h1>secret</h1>")
	blob, err := seal("hunter2hunter2hu", plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := open("hunter2hunter2hu", blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
	if _, err := open("wrong-password-xx", blob); err == nil {
		t.Fatal("wrong password decrypted (GCM tag should reject)")
	}
}

// Encrypt must embed a blob the (Go-mirrored) decryptor can actually open — this
// guards against the template and the sealing logic drifting apart.
func TestEncryptEmbedsDecryptableBlob(t *testing.T) {
	plain := []byte("<p>hello, behave!</p>")
	page, err := Encrypt("correct horse battery staple", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(page, []byte("<!doctype html>")) {
		t.Fatalf("page is not an HTML document: %q", page[:min(40, len(page))])
	}
	b64 := between(t, string(page), `const BLOB = "`, `"`)
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("embedded blob is not valid base64: %v", err)
	}
	got, err := open("correct horse battery staple", blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("decrypted embedded blob = %q", got)
	}
}

func TestWrapFSEncryptsOnlyHTML(t *testing.T) {
	const css = "body{color:red}"
	src := fstest.MapFS{
		"index.html":   {Data: []byte("<h1>top</h1>")},
		"style.css":    {Data: []byte(css)},
		"sub/page.htm": {Data: []byte("<h1>sub</h1>")},
	}
	wrapped := WrapFS(src, "a-long-enough-password")

	var names []string
	err := fs.WalkDir(wrapped, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, p)
		data, rerr := fs.ReadFile(wrapped, p)
		if rerr != nil {
			return rerr
		}
		if isHTML(p) {
			if !bytes.HasPrefix(data, []byte("<!doctype html>")) {
				t.Errorf("%s was not turned into a self-decrypting page", p)
			}
		} else if string(data) != css {
			t.Errorf("%s was modified; non-HTML must pass through untouched", p)
		}
		// Size reported by Stat must match the bytes served, or providers upload
		// a truncated/over-long object.
		info, _ := fs.Stat(wrapped, p)
		if info.Size() != int64(len(data)) {
			t.Errorf("%s Stat size %d != content length %d", p, info.Size(), len(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("walked %v, want all 3 files", names)
	}
}

func TestWrapFSEmptyPasswordIsPassthrough(t *testing.T) {
	src := fstest.MapFS{"index.html": {Data: []byte("<h1>x</h1>")}}
	if got := WrapFS(src, ""); !sameFS(got, src) {
		t.Fatal("empty password should return the original fs.FS unchanged")
	}
}

func sameFS(a, b fs.FS) bool {
	da, _ := fs.ReadFile(a, "index.html")
	db, _ := fs.ReadFile(b, "index.html")
	return string(da) == string(db)
}

func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("marker %q not found", start)
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("closing %q not found", end)
	}
	return rest[:j]
}
