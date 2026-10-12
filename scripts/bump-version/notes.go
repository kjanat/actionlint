package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const releaseNotesDir = "docs/releases"

func releaseNotesPath(tag string) string {
	return filepath.Join(releaseNotesDir, tag+".md")
}

func ReleaseNotes(root, tag string) (string, error) {
	path := releaseNotesPath(tag)
	content, err := os.ReadFile(filepath.Join(root, path))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s is missing; the release notes file publishes with %s", path, tag)
	}
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", path, err)
	}
	notes := strings.Trim(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if strings.TrimSpace(notes) == "" {
		return "", fmt.Errorf("%s is empty, so %s would be released without notes", path, tag)
	}
	return notes + "\n", nil
}

func CheckReleaseNotes(root, tag string, out io.Writer) error {
	if _, err := ReleaseNotes(root, tag); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "%s describes %s\n", releaseNotesPath(tag), tag)
	return nil
}
