package files

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
)

func TestPathExistsUsesContentProvider(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	present := filepath.Join(root, "virtual-empty.js")
	missing := filepath.Join(root, "missing.js")
	denied := filepath.Join(root, "denied.js")
	reads := 0
	var recorded []string
	reader := New(t.Context(), func(path string) ([]byte, error) {
		reads++
		switch path {
		case present:
			return nil, nil
		case denied:
			return nil, fs.ErrPermission
		default:
			return nil, fs.ErrNotExist
		}
	}, func(path string) { recorded = append(recorded, path) })
	for _, tc := range []struct {
		path   string
		exists bool
	}{{present, true}, {missing, false}} {
		exists, err := reader.PathExists(tc.path)
		if err != nil || exists != tc.exists {
			t.Fatalf("existence of %q: present=%t error=%v", tc.path, exists, err)
		}
	}
	if _, err := reader.PathExists(denied); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("access denial became absence: %v", err)
	}
	if reads != 3 || !slices.Equal(recorded, []string{present, missing}) {
		t.Fatalf("wrong provider reads or input records: reads=%d paths=%v", reads, recorded)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelled := New(ctx, func(string) ([]byte, error) { t.Fatal("canceled lookup read a file"); return nil, nil }, nil)
	if _, err := cancelled.PathExists(present); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
