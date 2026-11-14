// Package cache manages actionlint's downloaded and compiled dependencies.
package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/files"
)

type Entry struct {
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modified_at"`
	Protected  bool      `json:"protected"`
	info       fs.FileInfo
}

type Inventory struct {
	Directory string  `json:"directory"`
	Exists    bool    `json:"exists"`
	Files     int     `json:"files"`
	Bytes     int64   `json:"bytes"`
	Entries   []Entry `json:"entries"`
}

// Inspect lists files without opening their contents or following symbolic links.
func Inspect(ctx context.Context, directory string) (Inventory, error) {
	result := Inventory{Directory: directory, Entries: []Entry{}}
	root, err := open(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, ctx.Err()
	}
	if err != nil {
		return result, err
	}
	defer root.Close()
	result.Exists = true
	err = files.NewRooted(ctx, root, nil).WalkDir(".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		kind, _, _ := strings.Cut(name, "/")
		item := Entry{Path: name, Kind: kind, ModifiedAt: info.ModTime(), Protected: revoked(name), info: info}
		if info.Mode().IsRegular() {
			item.Bytes = info.Size()
		}
		result.Entries = append(result.Entries, item)
		result.Files++
		result.Bytes += item.Bytes
		return nil
	})
	return result, err
}

func open(directory string) (*os.Root, error) {
	if directory == "" {
		return nil, errors.New("could not determine the actionlint cache directory")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("cache directory must be a directory, not a symbolic link: %s", directory)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	current, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, current) {
		root.Close()
		return nil, fmt.Errorf("cache directory changed while opening %s", directory)
	}
	return root, nil
}

// Revocation records keep revoked plugins blocked during offline checks.
// They remain after cleanup, even when its downloaded artifact is removed.
func revoked(name string) bool {
	parts := strings.Split(name, "/")
	return len(parts) == 4 && parts[0] == "registry" && parts[2] == "revoked"
}

type Cleanup struct {
	Inventory
	DryRun bool `json:"dry_run"`
}

// Clean removes downloaded and compiled files. A nonzero cutoff prunes files
// last written before that time and keeps registry records intact, so cached
// version selections never lose their matching release records.
func Clean(ctx context.Context, directory string, cutoff time.Time, dryRun bool) (Cleanup, error) {
	inventory, err := Inspect(ctx, directory)
	result := Cleanup{Directory: directory, Exists: inventory.Exists, Entries: []Entry{}, DryRun: dryRun}
	if err != nil || !inventory.Exists {
		return result, err
	}
	root, err := open(directory)
	if err != nil {
		return result, err
	}
	defer root.Close()
	for _, entry := range inventory.Entries {
		if entry.Protected || !cutoff.IsZero() && (entry.Kind == "registry" || !entry.ModifiedAt.Before(cutoff)) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !dryRun {
			if err := removeUnchanged(root, entry); err != nil {
				return result, err
			}
		}
		result.Entries = append(result.Entries, entry)
		result.Files++
		result.Bytes += entry.Bytes
	}
	return result, nil
}

func removeUnchanged(root *os.Root, entry Entry) error {
	current, err := root.Lstat(entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(current, entry.info) || current.Size() != entry.info.Size() || !current.ModTime().Equal(entry.ModifiedAt) {
		return fmt.Errorf("cache file changed during cleanup: %s", entry.Path)
	}
	if err := root.Remove(entry.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Empty parent directories may be removed, but the cache root stays in place.
	for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
		err := root.Remove(parent)
		if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, os.ErrExist) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
