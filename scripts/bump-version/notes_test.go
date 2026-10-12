package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReleaseNotes(t *testing.T, root, tag, content string) {
	t.Helper()
	path := filepath.Join(root, releaseNotesPath(tag))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseNotesNormalizesLineEndings(t *testing.T) {
	root := t.TempDir()
	writeReleaseNotes(t, root, "v1.2.0", "\r\n# Title\r\n\r\n- change\r\n\r\n\r\n")
	got, err := ReleaseNotes(root, "v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Title\n\n- change\n"; got != want {
		t.Errorf("ReleaseNotes = %q, want %q", got, want)
	}
	writeReleaseNotes(t, root, "v1.2.1", "\n    indented code\nText  \n")
	got, err = ReleaseNotes(root, "v1.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "    indented code\nText  \n"; got != want {
		t.Errorf("ReleaseNotes = %q, want %q", got, want)
	}
}

func TestReleaseNotesRejectsMissingAndEmptyFiles(t *testing.T) {
	root := t.TempDir()
	_, err := ReleaseNotes(root, "v1.2.0")
	if err == nil || !strings.Contains(err.Error(), filepath.Join("docs", "releases", "v1.2.0.md")+" is missing") {
		t.Fatalf("missing file: got %v", err)
	}
	writeReleaseNotes(t, root, "v1.2.0", " \n\r\n")
	_, err = ReleaseNotes(root, "v1.2.0")
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty file: got %v", err)
	}
}

func TestCheckReleaseNotesReportsTheFile(t *testing.T) {
	root := t.TempDir()
	if err := CheckReleaseNotes(root, "v1.2.0", io.Discard); err == nil {
		t.Fatal("expected a missing file to fail the check")
	}
	writeReleaseNotes(t, root, "v1.2.0", "- change\n")
	var out bytes.Buffer
	if err := CheckReleaseNotes(root, "v1.2.0", &out); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("docs", "releases", "v1.2.0.md") + " describes v1.2.0\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestMainNotesPrintsTheReleaseNotesFile(t *testing.T) {
	root := t.TempDir()
	writeReleaseNotes(t, root, "v1.2.0", "Nice notes\r\n")
	var out, errOut bytes.Buffer
	if err := Main(context.Background(), []string{"bump-version", "-root", root, "-notes", "v1.2.0"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Nice notes\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestRepositoryReleaseNotesExistForTheDeclaredVersion(t *testing.T) {
	root := filepath.Join("..", "..")
	declared, err := Check(root, targets, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckReleaseNotes(root, "v"+declared, io.Discard); err != nil {
		t.Fatal(err)
	}
}
