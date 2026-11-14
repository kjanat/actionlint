package installation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/files"
)

func readCacheBytes(ctx context.Context, path string) ([]byte, error) {
	if err := safeCachePath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJSONBytes {
		return nil, errors.New("plugin cache record must be a regular file of at most 4 MiB")
	}
	f, err := files.New(ctx, nil, nil).Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJSONBytes {
		return nil, errors.New("plugin cache record must be a regular file of at most 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJSONBytes+1))
	if len(data) > maxJSONBytes {
		return nil, errors.New("plugin cache record exceeds 4 MiB")
	}
	return data, err
}

func physicalCachePath(absolute string) (string, error) {
	base := filepath.Dir(absolute)
	for {
		_, err := os.Lstat(base)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(base) == base {
			return "", err
		}
		base = filepath.Dir(base)
	}
	physical, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil {
		return "", err
	}
	return filepath.Join(physical, relative), nil
}

func safeCachePath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin cache record symlink refused: %s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
