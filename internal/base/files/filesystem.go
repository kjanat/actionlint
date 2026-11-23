package files

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

type Filesystem interface {
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]fs.DirEntry, error)
	Stat(string) (fs.FileInfo, error)
	Lstat(string) (fs.FileInfo, error)
	EvalSymlinks(string) (string, error)
	Glob(string) ([]string, error)
	Platform() string
}

type DiskFilesystem struct{}

type rootedFilesystem struct{ root *os.Root }

func (r rootedFilesystem) Open(path string) (*os.File, error)   { return r.root.Open(path) }
func (r rootedFilesystem) ReadFile(path string) ([]byte, error) { return r.root.ReadFile(path) }
func (r rootedFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.root.FS(), path)
}
func (r rootedFilesystem) Stat(path string) (fs.FileInfo, error)  { return r.root.Stat(path) }
func (r rootedFilesystem) Lstat(path string) (fs.FileInfo, error) { return r.root.Lstat(path) }
func (rootedFilesystem) EvalSymlinks(path string) (string, error) {
	return "", &fs.PathError{Op: "evalsymlinks", Path: path, Err: fs.ErrInvalid}
}
func (r rootedFilesystem) Glob(path string) ([]string, error) { return fs.Glob(r.root.FS(), path) }
func (rootedFilesystem) Platform() string                     { return runtime.GOOS }

func (DiskFilesystem) Open(path string) (*os.File, error) { return os.Open(path) }

func (DiskFilesystem) ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }
func (DiskFilesystem) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }
func (DiskFilesystem) Stat(path string) (fs.FileInfo, error)      { return os.Stat(path) }
func (DiskFilesystem) Lstat(path string) (fs.FileInfo, error)     { return os.Lstat(path) }
func (DiskFilesystem) EvalSymlinks(path string) (string, error)   { return filepath.EvalSymlinks(path) }
func (DiskFilesystem) Glob(path string) ([]string, error)         { return filepath.Glob(path) }
func (DiskFilesystem) Platform() string                           { return runtime.GOOS }

func NewFilesystem(ctx context.Context, filesystem Filesystem, record func(string)) *Reader {
	reader := New(ctx, filesystem.ReadFile, record)
	reader.filesystem = filesystem
	reader.exists = func(path string) error { _, err := filesystem.Stat(path); return err }
	return reader
}

func (r *Reader) Filesystem() Filesystem { return r.filesystem }
