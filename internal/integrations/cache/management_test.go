package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeEntry(t *testing.T, root, name string, modified time.Time) string {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(name), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, modified, modified); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestCacheLifecycle(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "actionlint")
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	names := []string{"artifacts/old", "wazero/old", "configs/old", "action-metadata/v2/old", "artifacts/recent", "registry/example/selections/latest.json", "registry/example/releases/v1.json", "registry/example/revoked/digest.json"}
	var bytes int64
	for _, name := range names {
		modified := old
		if name == "artifacts/recent" {
			modified = now
		}
		writeEntry(t, root, name, modified)
		bytes += int64(len(name))
	}
	inventory, err := Inspect(t.Context(), root)
	if err != nil || inventory.Files != len(names) || inventory.Bytes != bytes {
		t.Fatalf("inventory=%+v error=%v, want %d files and %d bytes", inventory, err, len(names), bytes)
	}
	cutoff := now.Add(-24 * time.Hour)
	preview, err := Clean(t.Context(), root, cutoff, true)
	if err != nil || preview.Files != 4 {
		t.Fatalf("prune preview=%+v error=%v", preview, err)
	}
	unchanged, err := Inspect(t.Context(), root)
	if err != nil || unchanged.Files != len(names) {
		t.Fatalf("dry run changed files: %+v error=%v", unchanged, err)
	}
	pruned, err := Clean(t.Context(), root, cutoff, false)
	if err != nil || pruned.Files != preview.Files || pruned.Bytes != preview.Bytes {
		t.Fatalf("prune=%+v error=%v, preview=%+v", pruned, err, preview)
	}
	remaining, err := Inspect(t.Context(), root)
	if err != nil || remaining.Files != 4 {
		t.Fatalf("remaining=%+v error=%v", remaining, err)
	}
	for _, entry := range remaining.Entries {
		if !slices.Contains(names[4:], entry.Path) {
			t.Errorf("unexpected remaining file %s", entry.Path)
		}
	}
	cleaned, err := Clean(t.Context(), root, time.Time{}, false)
	if err != nil || cleaned.Files != 3 {
		t.Fatalf("clean=%+v error=%v", cleaned, err)
	}
	remaining, err = Inspect(t.Context(), root)
	if err != nil || remaining.Files != 1 || !remaining.Entries[0].Protected || remaining.Entries[0].Path != names[7] {
		t.Fatalf("revocation record was not retained: %+v error=%v", remaining, err)
	}
	again, err := Clean(t.Context(), root, time.Time{}, false)
	if err != nil || again.Files != 0 {
		t.Fatalf("repeated clean=%+v error=%v", again, err)
	}
}

func TestCacheCleanupDoesNotFollowLinks(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	file := writeEntry(t, outside, "keep", time.Now())
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	inventory, err := Inspect(t.Context(), root)
	if err != nil || inventory.Files != 1 || inventory.Entries[0].Path != "linked-directory" || inventory.Bytes != 0 {
		t.Fatalf("followed cache link: %+v error=%v", inventory, err)
	}
	if _, err := Clean(t.Context(), root, time.Time{}, false); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(file); err != nil || string(contents) != "keep" {
		t.Fatalf("outside file changed: %q error=%v", contents, err)
	}
	alias := filepath.Join(t.TempDir(), "cache")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Clean(t.Context(), alias, time.Time{}, false); err == nil {
		t.Fatal("accepted linked cache root")
	}
}

func TestCacheCleanupRejectsRewrittenFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	file := writeEntry(t, directory, "artifacts/digest", time.Now())
	inventory, err := Inspect(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("new download"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := removeUnchanged(root, inventory.Entries[0]); err == nil {
		t.Fatal("removed a file rewritten after inventory")
	}
	if contents, err := os.ReadFile(file); err != nil || string(contents) != "new download" {
		t.Fatalf("rewritten file lost: %q error=%v", contents, err)
	}
}

func TestCacheMissingAndCancelled(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "missing")
	result, err := Clean(t.Context(), directory, time.Time{}, false)
	if err != nil || result.Exists || result.Files != 0 {
		t.Fatalf("missing cache=%+v error=%v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Clean(ctx, directory, time.Time{}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("missing cancellation: %v", err)
	}
}
