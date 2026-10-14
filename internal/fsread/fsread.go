// Package fsread is the only filesystem reader for analysis code. A Reader
// records every path whose content or listing it returned.
package fsread

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Contents func(path string) ([]byte, error)

func Disk(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func ProbeDir(path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

type Reader struct {
	ctx      context.Context
	contents Contents
	record   func(path string)
}

func New(ctx context.Context, contents Contents, record func(path string)) *Reader {
	if contents == nil {
		contents = Disk
	}
	if record == nil {
		record = func(string) {}
	}
	return &Reader{ctx: ctx, contents: contents, record: record}
}

func (r *Reader) Mapped(name func(path string) string) *Reader {
	return &Reader{ctx: r.ctx, contents: r.contents, record: func(path string) { r.record(name(path)) }}
}

func (r *Reader) Record(path string) {
	r.record(path)
}

func (r *Reader) ReadFile(path string) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	data, err := r.contents(path)
	if err != nil {
		return nil, err
	}
	r.record(path)
	return data, nil
}

func (r *Reader) Open(path string) (*os.File, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
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
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	r.record(path)
	return entries, nil
}

func (r *Reader) WalkDir(root string, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
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
