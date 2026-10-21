package pluginregistry

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLockReplacementPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits require a Unix filesystem")
	}
	for _, mode := range []os.FileMode{0o640, 0o444} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), LockName)
			if err := os.WriteFile(path, []byte(`{"schema_version":1,"plugins":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			file, err := OpenLock(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Fatalf("permissions=%o, want %o", info.Mode().Perm(), mode)
			}
		})
	}
}

// The second cancellation checkpoint follows the initial destination read.
// An external edit here deterministically exercises the final replacement guard.
type lockEditContext struct {
	context.Context
	check func()
}

func (c lockEditContext) Err() error {
	c.check()
	return c.Context.Err()
}

func TestLockReplacementRechecksConcurrentEdits(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "replace"
		if remove {
			name = "remove"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), LockName)
			original := []byte(`{"schema_version":1,"plugins":[]}`)
			edited := append(bytes.Clone(original), '\n', '\n')
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := OpenLock(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			checks := 0
			changed := false
			ctx := lockEditContext{t.Context(), func() {
				checks++
				if checks != 2 {
					return
				}
				if !remove {
					entries, err := filepath.Glob(filepath.Join(filepath.Dir(file.Path), ".actionlint-plugins-*"))
					if err != nil || len(entries) != 1 {
						t.Fatalf("replacement not staged: %v %v", entries, err)
					}
					staged, err := os.ReadFile(entries[0])
					if err != nil || len(staged) == 0 {
						t.Fatalf("replacement not written: %q %v", staged, err)
					}
				}
				if err := os.WriteFile(file.Path, edited, 0o600); err != nil {
					t.Fatal(err)
				}
				changed = true
			}}
			var replacement []byte
			if !remove {
				replacement = append(bytes.Clone(original), '\n')
			}
			err = file.replace(ctx, replacement)
			if !changed {
				t.Fatal("external edit was not exercised")
			}
			if err == nil || !strings.Contains(err.Error(), "changed during operation") {
				t.Fatalf("concurrent edit accepted: %v", err)
			}
			current, err := os.ReadFile(file.Path)
			if err != nil || !bytes.Equal(current, edited) {
				t.Fatalf("external edit lost: %q %v", current, err)
			}
			if !bytes.Equal(file.original, original) {
				t.Fatalf("transaction snapshot changed: %q", file.original)
			}
			entries, err := os.ReadDir(filepath.Dir(file.Path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("transaction debris: %v %v", entries, err)
			}
		})
	}
}

func TestLockReplacementPreservesReadOnlyWindowsFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows read-only replacement semantics")
	}
	path := filepath.Join(t.TempDir(), LockName)
	original := []byte(`{"schema_version":1,"plugins":[]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Error(err)
		}
	})
	file, err := OpenLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Save(t.Context()); err == nil {
		t.Fatal("replaced read-only destination")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("read-only lock changed: %q %v", current, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("read-only permissions changed: %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary read-only file leaked: %v %v", entries, err)
	}
}
