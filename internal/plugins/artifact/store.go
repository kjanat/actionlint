package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrCorrupt reports a stored blob whose bytes no longer match its digest,
// such as one left half-written by an interrupted download.
var ErrCorrupt = errors.New("stored artifact SHA256 mismatch")

// Store is the content-addressed artifact cache. The blob with SHA256 digest
// <hex> lives at Dir/<hex>; executables materialized from blobs live at
// Dir/exec/<hex>/<filename>. Every read re-hashes the bytes.
type Store struct {
	// Dir is the cache directory, Options.CacheDir.
	Dir string
}

// Read returns the blob with the SHA256 digest hex. It returns an error
// wrapping os.ErrNotExist when the blob is absent and ErrCorrupt when its
// bytes do not match the digest.
func (s Store) Read(ctx context.Context, digest string, limit int64) ([]byte, error) {
	if err := CheckDigest(digest); err != nil {
		return nil, err
	}
	if s.Dir == "" {
		return nil, fmt.Errorf("artifact %s: no cache directory: %w", digest, os.ErrNotExist)
	}
	data, err := readFile(ctx, filepath.Join(s.Dir, digest), limit)
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != digest {
		return nil, fmt.Errorf("artifact %s: %w", digest, ErrCorrupt)
	}
	return data, nil
}

// Publish adds data to the store under the SHA256 digest hex. It refuses
// bytes that do not match the digest, so they are never stored, and replaces
// a corrupt blob atomically.
func (s Store) Publish(ctx context.Context, digest string, data []byte) error {
	if err := CheckDigest(digest); err != nil {
		return err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != digest {
		return fmt.Errorf("artifact %s: %w", digest, errDigestMismatch)
	}
	if s.Dir == "" {
		return nil
	}
	path := filepath.Join(s.Dir, digest)
	if _, err := s.Read(ctx, digest, int64(len(data))); errors.Is(err, ErrCorrupt) {
		// cacheFile accepts an existing file only when it already holds
		// these bytes, so remove the corrupt one first.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return cacheFile(ctx, path, data)
}

// Materialize returns the path of an executable copy of the stored blob with
// the SHA256 digest hex, at Dir/exec/<hex>/<filename> with mode 0o755. On
// Windows it adds ".exe" to a filename without it, so os/exec runs the file.
// It re-hashes the stored blob and any existing copy on every call and
// rewrites a copy whose bytes changed.
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

// errDigestMismatch reports bytes that do not match the digest they were
// fetched or imported under.
var errDigestMismatch = errors.New("SHA256 mismatch")

// CheckDigest accepts a SHA256 digest written as 64 lowercase hex digits.
func CheckDigest(digest string) error {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
		return fmt.Errorf("invalid artifact digest %q: want 64 lowercase hex digits", digest)
	}
	return nil
}
