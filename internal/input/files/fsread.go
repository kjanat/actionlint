// Package files is the only filesystem reader for analysis code. A Reader
// records every path whose content, existence or listing it returned.
package files

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Contents supplies file bytes. Providers such as analysis.Options.ReadFile
// may return storage they retain; Reader.ReadFile detaches it.
type Contents func(path string) ([]byte, error)

func Disk(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func ProbeDir(path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

type Reader struct {
	root     *os.Root
	ctx      context.Context
	contents Contents
	exists   func(string) error
	record   func(path string)
}

func New(ctx context.Context, contents Contents, record func(path string)) *Reader {
	exists := func(path string) error {
		_, err := os.Stat(path)
		return err
	}
	if contents == nil {
		contents = Disk
	} else {
		provider := contents
		exists = func(path string) error {
			_, err := provider(path)
			return err
		}
	}
	if record == nil {
		record = func(string) {}
	}
	return &Reader{ctx: ctx, contents: contents, exists: exists, record: record}
}

func (r *Reader) Mapped(name func(path string) string) *Reader {
	return &Reader{root: r.root, ctx: r.ctx, contents: r.contents, exists: r.exists, record: func(path string) { r.record(name(path)) }}
}

// NewRooted reads paths relative to root and rejects paths outside it. The caller
// owns root and must keep it open while the reader is in use.
func NewRooted(ctx context.Context, root *os.Root, record func(string)) *Reader {
	reader := New(ctx, root.ReadFile, record)
	reader.root = root
	reader.exists = func(name string) error {
		_, err := root.Stat(name)
		return err
	}
	return reader
}

// PathExists checks the configured provider and records both present and missing
// paths. Disk readers inspect metadata; content providers can establish existence
// by reading, including an empty file. Other failures do not establish absence.
func (r *Reader) PathExists(path string) (bool, error) {
	if err := r.ctx.Err(); err != nil {
		return false, err
	}
	err := r.exists(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	r.record(path)
	return err == nil, nil
}

func (r *Reader) Record(path string) {
	r.record(path)
}

// ReadFile returns bytes the caller owns, whatever the provider retains.
func (r *Reader) ReadFile(path string) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.contents(path)
	if err != nil {
		return nil, err
	}
	r.record(path)
	return bytes.Clone(data), nil
}

func (r *Reader) Open(path string) (*os.File, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	open := os.Open
	if r.root != nil {
		open = r.root.Open
	}
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	r.record(path)
	return file, nil
}

func (r *Reader) ReadDir(path string) ([]fs.DirEntry, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	var err error
	if r.root == nil {
		entries, err = os.ReadDir(path)
	} else {
		var directory *os.File
		directory, err = r.root.Open(path)
		if err == nil {
			entries, err = directory.ReadDir(-1)
			err = errors.Join(err, directory.Close())
			slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		}
	}
	if err != nil {
		return nil, err
	}
	r.record(path)
	return entries, nil
}

func (r *Reader) WalkDir(root string, fn fs.WalkDirFunc) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	lstat := os.Lstat
	if r.root != nil {
		lstat = r.root.Lstat
	}
	info, err := lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = r.walk(root, fs.FileInfoToDirEntry(info), fn)
	}
	if errors.Is(err, filepath.SkipDir) || errors.Is(err, filepath.SkipAll) {
		return nil
	}
	return err
}

func (r *Reader) walk(path string, entry fs.DirEntry, fn fs.WalkDirFunc) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if err := fn(path, entry, nil); err != nil || !entry.IsDir() {
		if errors.Is(err, filepath.SkipDir) && entry.IsDir() {
			return nil
		}
		return err
	}
	entries, err := r.ReadDir(path)
	if err != nil {
		if err := fn(path, entry, err); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				return nil
			}
			return err
		}
	}
	for _, child := range entries {
		if err := r.walk(filepath.Join(path, child.Name()), child, fn); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				break
			}
			return err
		}
	}
	return nil
}
