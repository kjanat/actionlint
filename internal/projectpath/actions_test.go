package projectpath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/fsread"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestActionsReportReadDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "a/b/action.yml", "name: fixture\n")
	writeFixture(t, root, "node_modules/x/action.yml", "name: fixture\n")
	writeFixture(t, root, "nested/.git", "gitdir: elsewhere\n")
	writeFixture(t, root, ".github/workflows/action.yml", "on: push\n")
	var got []string
	paths, err := Actions(t.Context(), root, fsread.New(t.Context(), nil, func(path string) { got = append(got, path) }))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "a", "b", "action.yml")}; !slices.Equal(paths, want) {
		t.Fatalf("Actions = %q, want %q", paths, want)
	}
	slices.Sort(got)
	want := []string{root, filepath.Join(root, ".github"), filepath.Join(root, "a"), filepath.Join(root, "a", "b")}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("reported %q, want %q", got, want)
	}
}

func TestActionsReportLogicalDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "a/action.yml", "name: fixture\n")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var got []string
	paths, err := Actions(t.Context(), alias, fsread.New(t.Context(), nil, func(path string) { got = append(got, path) }))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(alias, "a", "action.yml")}; !slices.Equal(paths, want) {
		t.Fatalf("Actions = %q, want %q", paths, want)
	}
	slices.Sort(got)
	if want := []string{alias, filepath.Join(alias, "a")}; !slices.Equal(got, want) {
		t.Fatalf("reported %q, want %q", got, want)
	}
}

func TestActionsCancelled(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "action.yml", "name: fixture\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Actions(ctx, root, fsread.New(ctx, nil, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Actions = %v, want context.Canceled", err)
	}
}
