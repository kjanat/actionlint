package capture

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestRevisionLookupRequiresSealingAndPerformsNoIO(t *testing.T) {
	t.Parallel()
	var reads, resolves atomic.Int32
	path := filepath.Join(t.TempDir(), "ci.yml")
	store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) { reads.Add(1); return []byte("abc"), nil }, func(path string) string { resolves.Add(1); return filepath.ToSlash(path) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	snapshot, err := store.Read(t.Context(), path, sourcetext.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Revision(snapshot.Revision()); ok {
		t.Fatal("unsealed revision exposed")
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	readCount, resolveCount := reads.Load(), resolves.Load()
	got, ok := store.Revision(snapshot.Revision())
	if !ok || got.Revision() != snapshot.Revision() || got.Coordinates() != sourcetext.YAML {
		t.Fatal("sealed revision unavailable")
	}
	data := got.Bytes()
	data[0] = 'x'
	again, ok := store.Revision(snapshot.Revision())
	if !ok || string(again.Bytes()) != "abc" {
		t.Fatal("snapshot bytes were mutable")
	}
	stale, err := sourcetext.NewRevision(snapshot.Revision().Document(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Revision(stale); ok {
		t.Fatal("stale revision accepted")
	}
	if _, ok := store.Revision(sourcetext.Revision{}); ok {
		t.Fatal("zero revision accepted")
	}
	if reads.Load() != readCount || resolves.Load() != resolveCount {
		t.Fatal("sealed revision lookup performed IO")
	}
}
