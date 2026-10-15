package genfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFormatterFailurePreservesDestination(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, exclusive := range []bool{false, true} {
			t.Run(fmtTestName(exists, exclusive), func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("PATH", root)
				path := filepath.Join(root, "test.schema.json")
				if exists {
					if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				err := Write(t.Context(), path, []byte(`{"type":"object"}`), exclusive)
				if exists && exclusive {
					if !errors.Is(err, os.ErrExist) {
						t.Fatalf("expected existing snapshot error, got %v", err)
					}
				} else if !errors.Is(err, exec.ErrNotFound) {
					t.Fatalf("expected missing formatter error, got %v", err)
				}
				content, err := os.ReadFile(path)
				if exists {
					if err != nil || string(content) != "original\n" {
						t.Fatalf("destination changed: %q, %v", content, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("failed generation created destination: %v", err)
				}
			})
		}
	}
}

func fmtTestName(exists, exclusive bool) string {
	name := "new"
	if exists {
		name = "existing"
	}
	if exclusive {
		name += "/snapshot"
	} else {
		name += "/replace"
	}
	return name
}

func TestWriteNewFileRefusesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.schema.json")
	if err := writeNewFile(path, []byte("original\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeNewFile(path, []byte("replacement\n")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected exclusive creation error, got %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "original\n" {
		t.Fatalf("snapshot changed: %q, %v", content, err)
	}
}
