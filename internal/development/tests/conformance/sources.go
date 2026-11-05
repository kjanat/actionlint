// Package conformance loads the pinned upstream test corpora without extracting or executing their code.
package conformance

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
)

//go:embed sources.json
var manifest []byte

// Source identifies a repository archive and its content digest.
type Source struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
	SHA256     string `json:"sha256"`
}

// Sources reads and validates the checked-in source revisions.
func Sources() ([]Source, error) {
	var sources []Source
	if err := json.Unmarshal(manifest, &sources); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if seen[s.Name] || !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(s.Name) ||
			!regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(s.Repository) ||
			!regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(s.Revision) ||
			!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(s.SHA256) {
			return nil, fmt.Errorf("invalid or duplicate conformance source %q", s.Name)
		}
		seen[s.Name] = true
	}
	return sources, nil
}

// ArchivePath returns the cache filename for a source revision.
func (s Source) ArchivePath(dir string) string {
	return filepath.Join(dir, s.Name+"-"+s.Revision+".zip")
}

func (s Source) verify(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != s.SHA256 {
		return fmt.Errorf("%s: SHA-256 mismatch: got %s, want %s", filename, got, s.SHA256)
	}
	return nil
}

// Fetch downloads a missing archive. Existing archives must still match their pinned digest.
func (s Source) Fetch(ctx context.Context, dir string) error {
	filename := s.ArchivePath(dir)
	if _, err := os.Stat(filename); err == nil {
		return s.verify(filename)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://codeload.github.com/"+s.Repository+"/zip/"+s.Revision, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", s.Repository, resp.Status)
	}
	f, err := os.CreateTemp(dir, ".download-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	const maxSize = 256 << 20
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxSize {
		return fmt.Errorf("%s archive exceeds %d bytes", s.Name, maxSize)
	}
	if err := s.verify(f.Name()); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

// Open verifies an archive and exposes its repository root as a read-only filesystem.
func (s Source) Open(dir string) (fs.FS, func() error, error) {
	filename := s.ArchivePath(dir)
	if err := s.verify(filename); err != nil {
		return nil, nil, err
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, nil, err
	}
	root, err := fs.Sub(z, path.Base(s.Repository)+"-"+s.Revision)
	if err != nil {
		z.Close()
		return nil, nil, err
	}
	if _, err := fs.Stat(root, "."); err != nil {
		z.Close()
		return nil, nil, fmt.Errorf("%s archive has no expected repository root: %w", s.Name, err)
	}
	return root, z.Close, nil
}
