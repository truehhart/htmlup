// Package htmlcrypt turns an HTML document into a self-decrypting page: the
// document is encrypted with a password and embedded in a small wrapper that
// prompts for that password and decrypts in-browser via the WebCrypto API.
//
// The threat model is deliberately modest. The ciphertext ships to the client,
// so anyone with the page and the password can read it, and an offline
// brute-force is cheap — this gates casual access (share-link protection), it
// is not confidentiality against a determined attacker. Password strength is
// the only real defense; use a long one.
//
// The Go encryptor and the browser decryptor must agree byte-for-byte: AES-256
// in GCM, PBKDF2-SHA256 key derivation, and the blob layout salt(16) ||
// nonce(12) || ciphertext+tag, base64-encoded. The iteration count is injected
// into the template from the constant below so the two sides cannot drift.
package htmlcrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"io"
	"io/fs"
	"path"
	"strings"
	"text/template"
	"time"
)

const (
	iterations = 600_000 // PBKDF2-SHA256 rounds; matched in the browser template
	keyLen     = 32      // AES-256
	saltLen    = 16
	nonceLen   = 12 // GCM standard nonce
)

//go:embed templates/page.html
var pageTmplSrc string

var pageTmpl = template.Must(template.New("page").Parse(pageTmplSrc))

// Encrypt wraps htmlDoc in a self-decrypting page protected by password.
func Encrypt(password string, htmlDoc []byte) ([]byte, error) {
	blob, err := seal(password, htmlDoc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = pageTmpl.Execute(&buf, struct {
		Blob       string
		Iterations int
	}{base64.StdEncoding.EncodeToString(blob), iterations})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// seal derives a key from password and returns salt || nonce || ciphertext+tag.
func seal(password string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	gcm, err := newGCM(password, salt)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	blob := make([]byte, 0, saltLen+nonceLen+len(ct))
	blob = append(blob, salt...)
	blob = append(blob, nonce...)
	return append(blob, ct...), nil
}

// open reverses seal — used only by tests to assert the round-trip; the real
// decryption happens in the browser.
func open(password string, blob []byte) ([]byte, error) {
	salt, nonce, ct := blob[:saltLen], blob[saltLen:saltLen+nonceLen], blob[saltLen+nonceLen:]
	gcm, err := newGCM(password, salt)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ct, nil)
}

func newGCM(password string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func isHTML(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// WrapFS returns fsys unchanged when password is empty. Otherwise every .html /
// .htm file opened through it is transparently replaced with its self-decrypting
// page; all other files (CSS, JS, images) pass through untouched. Encryption
// happens lazily on Open with fresh salt/nonce per call — no shared state, so
// concurrent reads (the S3 backend uploads in parallel) are safe.
func WrapFS(fsys fs.FS, password string) fs.FS {
	if password == "" {
		return fsys
	}
	return encFS{inner: fsys, password: password}
}

type encFS struct {
	inner    fs.FS
	password string
}

func (e encFS) Open(name string) (fs.File, error) {
	f, err := e.inner.Open(name)
	if err != nil {
		return nil, err
	}
	if !isHTML(name) {
		return f, nil
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() { // a directory literally named "x.html" — leave it alone
		return f, nil
	}
	plain, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	page, err := Encrypt(e.password, plain)
	if err != nil {
		return nil, err
	}
	return &memFile{name: path.Base(name), data: page, modtime: info.ModTime(), reader: bytes.NewReader(page)}, nil
}

// memFile serves the encrypted page from memory. Stat().Size() reflects the
// encrypted length so providers (S3 ContentLength, the git blob) upload the
// right byte count, and Seek satisfies the io.Seeker the AWS SDK asserts on to
// sign and retry the request body.
type memFile struct {
	name    string
	data    []byte
	modtime time.Time
	reader  *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return memInfo{f.name, int64(len(f.data)), f.modtime}, nil
}
func (f *memFile) Read(b []byte) (int, error)                { return f.reader.Read(b) }
func (f *memFile) Seek(off int64, whence int) (int64, error) { return f.reader.Seek(off, whence) }
func (f *memFile) Close() error                              { return nil }

type memInfo struct {
	name    string
	size    int64
	modtime time.Time
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return i.modtime }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }
