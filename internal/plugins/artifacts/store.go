package artifacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

var ErrCorrupt = errors.New("stored artifact SHA256 mismatch")

// Store is the content-addressed artifact cache. The blob with SHA256 digest
// <hex> lives at Dir/<hex>; executables materialized from blobs live at
// Dir/exec/<hex>/<filename>. Every read re-hashes the bytes.
type Store struct {
	Dir string
}

func (s Store) Read(ctx context.Context, digest string, limit int64) ([]byte, error) {
	if err := pluginreference.CheckDigest(digest); err != nil {
		return nil, err
	}
	if s.Dir == "" {
		return nil, fmt.Errorf("artifact %s: no cache directory: %w", digest, os.ErrNotExist)
	}
	data, err := readFile(ctx, filepath.Join(s.Dir, digest), limit)
	if err != nil {
		return nil, err
	}
	if sha256hex.Digest(data) != digest {
		return nil, fmt.Errorf("artifact %s: %w", digest, ErrCorrupt)
	}
	return data, nil
}

func (s Store) Publish(ctx context.Context, digest string, data []byte) error {
	if err := pluginreference.CheckDigest(digest); err != nil {
		return err
	}
	if sha256hex.Digest(data) != digest {
		return fmt.Errorf("artifact %s: %w", digest, errDigestMismatch)
	}
	if s.Dir == "" {
		return nil
	}
	path := filepath.Join(s.Dir, digest)
	if _, err := s.Read(ctx, digest, int64(len(data))); err == nil {
		return nil
	}
	return cacheFile(ctx, path, data)
}

func (s Store) Materialize(ctx context.Context, digest, filename string, limit int64) (string, error) {
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\:`) {
		return "", fmt.Errorf("invalid executable filename %q", filename)
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(filename), ".exe") {
		filename += ".exe"
	}
	data, err := s.Read(ctx, digest, limit)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(s.Dir, "exec", digest)
	path := filepath.Join(directory, filename)
	if existing, err := readFile(ctx, path, limit); err == nil && bytes.Equal(existing, data) {
		return path, nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(directory, ".materialize-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Chmod(0o755), f.Close()); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

var errDigestMismatch = errors.New("SHA256 mismatch")
