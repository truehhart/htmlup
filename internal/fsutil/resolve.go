package fsutil

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func ResolveFS(path string) (fs.FS, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return os.DirFS(path), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &singleFileFS{name: filepath.Base(path), data: data}, nil
}

type singleFileFS struct {
	name string
	data []byte
}

func (s *singleFileFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &rootDir{entry: s}, nil
	}
	if name == s.name {
		return &memFile{info: s, reader: bytes.NewReader(s.data)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (s *singleFileFS) Name() string               { return s.name }
func (s *singleFileFS) Size() int64                { return int64(len(s.data)) }
func (s *singleFileFS) Mode() fs.FileMode          { return 0o444 }
func (s *singleFileFS) ModTime() time.Time         { return time.Time{} }
func (s *singleFileFS) IsDir() bool                { return false }
func (s *singleFileFS) Sys() any                   { return nil }
func (s *singleFileFS) Type() fs.FileMode          { return 0 }
func (s *singleFileFS) Info() (fs.FileInfo, error) { return s, nil }

type memFile struct {
	info   *singleFileFS
	reader *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(b []byte) (int, error) { return f.reader.Read(b) }
func (f *memFile) Close() error               { return nil }

type rootDir struct {
	entry *singleFileFS
	read  bool
}

func (d *rootDir) Stat() (fs.FileInfo, error) { return dirInfo{}, nil }

func (d *rootDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}

func (d *rootDir) Close() error { return nil }

func (d *rootDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.read {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	d.read = true
	return []fs.DirEntry{d.entry}, nil
}

type dirInfo struct{}

func (dirInfo) Name() string       { return "." }
func (dirInfo) Size() int64        { return 0 }
func (dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (dirInfo) ModTime() time.Time { return time.Time{} }
func (dirInfo) IsDir() bool        { return true }
func (dirInfo) Sys() any           { return nil }
