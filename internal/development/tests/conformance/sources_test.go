package conformance

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func fixtureArchive(t *testing.T, root string) (Source, string) {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	f, err := z.Create(root + "/fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("upstream fixture")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	s := Source{Name: "fixture", Repository: "owner/suite", Revision: strings.Repeat("a", 40), SHA256: fmt.Sprintf("%x", sha256.Sum256(data.Bytes()))}
	dir := t.TempDir()
	if err := os.WriteFile(s.ArchivePath(dir), data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestSourceArchiveVerification(t *testing.T) {
	s, dir := fixtureArchive(t, "suite-"+strings.Repeat("a", 40))
	root, closeSource, err := s.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := fs.ReadFile(root, "fixture.txt")
	closeErr := closeSource()
	if readErr != nil || closeErr != nil || string(data) != "upstream fixture" {
		t.Fatalf("read verified fixture: %q, %v, %v", data, readErr, closeErr)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Fetch(ctx, dir); err != nil {
		t.Fatalf("verified cached archive must work without a network request: %v", err)
	}
	if err := os.WriteFile(s.ArchivePath(dir), []byte("corrupted cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, closeSource, err := s.Open(dir); err == nil {
		_ = closeSource()
		t.Fatal("corrupted archive was accepted")
	} else if !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatal(err)
	}
	if err := s.Fetch(ctx, dir); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("corrupted cache must fail verification without a replacement download: %v", err)
	}
}

func TestSourceArchiveMissingRoot(t *testing.T) {
	s, dir := fixtureArchive(t, "wrong-root")
	if _, closeSource, err := s.Open(dir); err == nil {
		_ = closeSource()
		t.Fatal("archive with wrong repository root was accepted")
	}
	if _, _, err := s.Open(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing archive should require fetching: %v", err)
	}
}
