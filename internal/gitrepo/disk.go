package gitrepo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"
)

type disk struct {
	ctx    context.Context
	record func(path string)
}

func (r *Repository) disk(ctx context.Context) disk {
	return disk{ctx: ctx, record: r.reads.record}
}

func (d disk) readFile(path string) ([]byte, error) {
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d.record(path)
	return data, nil
}

func (d disk) readOptional(path string) ([]byte, error) {
	data, err := d.readFile(path)
	if missing(err) {
		return nil, nil
	}
	return data, err
}

func (d disk) open(path string) (*os.File, error) {
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d.record(path)
	return file, nil
}

func (d disk) readDir(path string) ([]fs.DirEntry, error) {
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	d.record(path)
	return entries, nil
}

func (d disk) readlink(path string) (string, error) {
	if err := d.ctx.Err(); err != nil {
		return "", err
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	d.record(path)
	return target, nil
}

func (d disk) wait(delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-d.ctx.Done():
		return d.ctx.Err()
	case <-timer.C:
		return nil
	}
}

func missing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func permissionDenied(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return errors.Is(err, fs.ErrPermission)
	}
	_ = file.Close()
	return false
}
