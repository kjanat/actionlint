package fsread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SnapshotFS exposes bounded, read-only file snapshots beneath an open root.
// Every lookup is root-contained, including when symlinks change concurrently.
type SnapshotFS struct {
	ctx    context.Context
	root   *os.Root
	reader *Reader
	mu     sync.Mutex
	files  map[string]*snapshotData
	names  map[string]snapshotName
	budget *SnapshotBudget
}

// SnapshotBudget bounds all directory grants participating in one invocation.
type SnapshotBudget struct {
	mu             sync.Mutex
	bytes          int64
	files, lookups int
	failure        budgetError
}

// budgetError is the condition that invalidated a budget; every grant
// sharing the budget reports it from Close. The zero value means none.
type budgetError string

const (
	errIdentityChanged budgetError = "plugin source identity changed during lookup"
	errTooManyFiles    budgetError = "plugin source files exceed 256 files"
	errTooManyBytes    budgetError = "plugin source files exceed 8 MiB"
	errTooManyLookups  budgetError = "plugin source files exceed 4096 lookups"
)

func (f budgetError) Error() string { return string(f) }

// fail records failure and returns it. The caller holds b.mu.
func (b *SnapshotBudget) fail(failure budgetError) error {
	b.failure = failure
	return failure
}

// err returns the recorded failure, or nil. The caller holds b.mu.
func (b *SnapshotBudget) err() error {
	if b.failure == "" {
		return nil
	}
	return b.failure
}

type snapshotData struct {
	content []byte
	info    fileInfo
}

// fileInfo is the metadata a guest sees: plain values detached from the host
// stat result, whose Sys buffer would otherwise be shared by every holder.
type fileInfo struct {
	name                 string
	size                 int64
	mode                 fs.FileMode
	seconds, nanoseconds int64
}

func detachInfo(info fs.FileInfo) fileInfo {
	modified := info.ModTime()
	return fileInfo{name: info.Name(), size: info.Size(), mode: info.Mode(), seconds: modified.Unix(), nanoseconds: int64(modified.Nanosecond())}
}

func (i fileInfo) Name() string       { return i.name }
func (i fileInfo) Size() int64        { return i.size }
func (i fileInfo) Mode() fs.FileMode  { return i.mode }
func (i fileInfo) ModTime() time.Time { return time.Unix(i.seconds, i.nanoseconds) }
func (i fileInfo) IsDir() bool        { return i.mode.IsDir() }
func (fileInfo) Sys() any             { return nil }

type snapshotName struct {
	path string
	info fs.FileInfo
}

// NewSnapshot opens an explicit directory grant. Close it after guest teardown.
func NewSnapshot(ctx context.Context, directory string, reader *Reader, budget *SnapshotBudget) (*SnapshotFS, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return newSnapshot(ctx, root, reader, budget), nil
}

// NewSnapshotAt opens a child grant through an already authorized root. The
// parent stays caller-owned; the snapshot owns its independently opened child.
func NewSnapshotAt(ctx context.Context, parent *os.Root, name string, reader *Reader, budget *SnapshotBudget) (*SnapshotFS, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return newSnapshot(ctx, root, reader, budget), nil
}

func newSnapshot(ctx context.Context, root *os.Root, reader *Reader, budget *SnapshotBudget) *SnapshotFS {
	if budget == nil {
		budget = &SnapshotBudget{}
	}
	return &SnapshotFS{ctx: ctx, root: root, reader: reader, files: make(map[string]*snapshotData), names: make(map[string]snapshotName), budget: budget}
}

func (s *SnapshotFS) Close() error {
	s.budget.mu.Lock()
	defer s.budget.mu.Unlock()
	return errors.Join(s.root.Close(), s.budget.err())
}

func (s *SnapshotFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return s.open(name)
}

func (s *SnapshotFS) open(name string) (fs.File, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resolved, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	name = resolved.path
	if data := s.files[name]; data != nil {
		return &snapshotFile{Reader: bytes.NewReader(data.content), info: data.info}, nil
	}
	if err := s.lookup(name); err != nil {
		return nil, err
	}
	info, err := s.root.Stat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, fs.ErrPermission
	}
	file, err := openSnapshotFile(s.root, filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !os.SameFile(resolved.info, info) {
		s.budget.mu.Lock()
		err = s.budget.fail(errIdentityChanged)
		s.budget.mu.Unlock()
		return nil, errors.Join(err, file.Close())
	}
	if info.IsDir() {
		if err := file.Close(); err != nil {
			return nil, err
		}
		return snapshotDirectory{fs: s, name: name, info: detachInfo(info)}, nil
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(fs.ErrPermission, file.Close())
	}
	s.budget.mu.Lock()
	defer s.budget.mu.Unlock()
	if s.budget.files >= 256 {
		return nil, errors.Join(s.budget.fail(errTooManyFiles), file.Close())
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit-s.budget.bytes+1))
	err = errors.Join(err, file.Close())
	if err != nil {
		return nil, err
	}
	if int64(len(data))+s.budget.bytes > limit {
		return nil, s.budget.fail(errTooManyBytes)
	}
	s.budget.bytes += int64(len(data))
	s.budget.files++
	detached := detachInfo(info)
	s.files[name] = &snapshotData{content: data, info: detached}
	s.reader.Record(filepath.Join(s.root.Name(), filepath.FromSlash(name)))
	return &snapshotFile{Reader: bytes.NewReader(data), info: detached}, nil
}

func (s *SnapshotFS) lookup(name string) error {
	s.budget.mu.Lock()
	defer s.budget.mu.Unlock()
	if s.budget.lookups >= 4096 {
		return s.budget.fail(errTooManyLookups)
	}
	s.budget.lookups++
	s.reader.Record(filepath.Join(s.root.Name(), filepath.FromSlash(name)))
	return nil
}

// ReadFile reads name through the snapshot: the bytes the guest read if it
// read the file, otherwise a new root-contained read within the budget. It
// records the read like a guest's.
func (s *SnapshotFS) ReadFile(name string) ([]byte, error) {
	file, err := s.Open(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	// Open snapshots a regular file in full; a directory has no content.
	_, data, ok := s.Content(name)
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	return data, nil
}

// Content returns the resolved name and exact bytes read by the guest.
func (s *SnapshotFS) Content(name string) (string, []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = s.names[sourceSpelling(name)].path
	data, ok := s.files[name]
	if !ok {
		return "", nil, false
	}
	return name, bytes.Clone(data.content), true
}

// Resolve symlinks before processing '..' so dependency paths match filesystem traversal.
// os.Root confines every metadata lookup and the subsequent file open.
func (s *SnapshotFS) resolve(name string) (snapshotName, error) {
	if path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return snapshotName{}, fs.ErrPermission
	}
	name = sourceSpelling(name)
	if resolved, ok := s.names[name]; ok {
		return resolved, nil
	}
	original := name
	pending := strings.Split(name, "/")
	var parts []string
	var identities []fs.FileInfo
	links := 0
	for len(pending) != 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) == 0 {
				return snapshotName{}, fs.ErrPermission
			}
			parts = parts[:len(parts)-1]
			identities = identities[:len(identities)-1]
			continue
		}
		candidate := strings.Join(append(parts, part), "/")
		if err := s.lookup(candidate); err != nil {
			return snapshotName{}, err
		}
		info, err := s.root.Lstat(filepath.FromSlash(candidate))
		if err != nil {
			return snapshotName{}, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			links++
			if links > 40 {
				return snapshotName{}, errors.New("plugin source path exceeds 40 symlinks")
			}
			target, err := s.root.Readlink(filepath.FromSlash(candidate))
			if err != nil {
				return snapshotName{}, err
			}
			if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				return snapshotName{}, fs.ErrPermission
			}
			pending = append(strings.Split(filepath.ToSlash(target), "/"), pending...)
			continue
		}
		if len(pending) != 0 && !info.IsDir() {
			return snapshotName{}, fs.ErrInvalid
		}
		parts = append(parts, part)
		identities = append(identities, info)
	}
	resolved := strings.Join(parts, "/")
	var info fs.FileInfo
	if resolved == "" {
		resolved = "."
		var err error
		info, err = s.root.Stat(".")
		if err != nil {
			return snapshotName{}, err
		}
	} else {
		info = identities[len(identities)-1]
	}
	entry := snapshotName{path: resolved, info: info}
	s.names[original] = entry
	s.names[resolved] = entry
	return entry, nil
}

func sourceSpelling(name string) string {
	parts := strings.Split(name, "/")
	filtered := parts[:0]
	for _, part := range parts {
		if part != "." && part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "/")
}

type snapshotFile struct {
	*bytes.Reader
	info fileInfo
}

func (f *snapshotFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (*snapshotFile) Close() error                 { return nil }

type snapshotDirectory struct {
	fs   *SnapshotFS
	name string
	info fileInfo
}

func (d snapshotDirectory) Stat() (fs.FileInfo, error) { return d.info, nil }
func (snapshotDirectory) Read([]byte) (int, error)     { return 0, fs.ErrInvalid }
func (snapshotDirectory) Close() error                 { return nil }

func (d snapshotDirectory) resolve(name string) (string, error) {
	if path.IsAbs(name) {
		return "", fs.ErrPermission
	}
	return d.name + "/" + name, nil
}

func (d snapshotDirectory) OpenAt(name string, flag int, _ fs.FileMode) (fs.File, error) {
	if flag != os.O_RDONLY {
		return nil, fs.ErrPermission
	}
	name, err := d.resolve(name)
	if err != nil {
		return nil, err
	}
	return d.fs.open(name)
}

func (d snapshotDirectory) StatAt(name string) (fs.FileInfo, error) {
	name, err := d.resolve(name)
	if err != nil {
		return nil, err
	}
	if err := d.fs.ctx.Err(); err != nil {
		return nil, err
	}
	// Missing search candidates also affect which source will be selected.
	d.fs.mu.Lock()
	defer d.fs.mu.Unlock()
	resolved, err := d.fs.resolve(name)
	if err != nil {
		return nil, err
	}
	name = resolved.path
	if err := d.fs.lookup(name); err != nil {
		return nil, err
	}
	if data := d.fs.files[name]; data != nil {
		return data.info, nil
	}
	info, err := d.fs.root.Stat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	return detachInfo(info), nil
}
