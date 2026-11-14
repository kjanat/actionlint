package source

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func configCachePath(ctx context.Context, name string) string {
	directory := network.Settings(ctx).CacheDirectory
	if directory == "" {
		return ""
	}
	return filepath.Join(directory, "configs", fmt.Sprintf("%x", sha256.Sum256([]byte(name))))
}

func readCachedConfig(ctx context.Context, name string) ([]byte, error) {
	path := configCachePath(ctx, name)
	if path == "" {
		return nil, network.Missing(name)
	}
	file, err := files.New(ctx, nil, nil).Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, network.Missing(name)
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > MaxBytes {
		return nil, fmt.Errorf("cached config exceeds %d bytes", MaxBytes)
	}
	return content, ctx.Err()
}

func cacheConfig(ctx context.Context, name string, content []byte) (err error) {
	path := configCachePath(ctx, name)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close(), ctx.Err()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
