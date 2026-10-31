// Package capturefixture supplies result-owned source captures to tests.
package capturefixture

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/source"
)

// CapturedSources supplies a test filesystem to one immutable capture owner.
// Relative fixture names resolve against the explicitly supplied absolute base.
// Updating fixture bytes never changes an already captured revision.
type CapturedSources struct {
	store   *capture.Store
	base    string
	ctx     context.Context
	mu      sync.Mutex
	content map[string][]byte
}

// NewCapturedSources returns a capture store and the fixture that feeds it.
// The store reads bytes passed to Capture first and the filesystem otherwise.
// The fixture keeps a private reference to the same store for Capture, so a
// caller cannot point Capture at a different store. The store is closed when
// the test ends.
func NewCapturedSources(t testing.TB, base string, overlays ...capture.Overlay) (*capture.Store, *CapturedSources) {
	t.Helper()
	if !filepath.IsAbs(base) {
		t.Fatalf("capture fixture requires an absolute base: %q", base)
	}
	fixture := &CapturedSources{base: base, ctx: t.Context(), content: make(map[string][]byte)}
	reader := fsread.New(t.Context(), nil, nil)
	store, err := capture.New(t.Context(), overlays, func(_ context.Context, document string) ([]byte, error) {
		fixture.mu.Lock()
		content, exists := fixture.content[document]
		content = bytes.Clone(content)
		fixture.mu.Unlock()
		if exists {
			return content, nil
		}
		return reader.ReadFile(document)
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.store = store
	CloseCaptures(t, store)
	return store, fixture
}

// CurrentDir returns the absolute working directory for relative-path fixtures.
func CurrentDir(t testing.TB) string {
	t.Helper()
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func (f *CapturedSources) Path(path string) string {
	if path == "<stdin>" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(f.base, path)
}

func (f *CapturedSources) Capture(path string, content []byte, coordinates source.Coordinates) (capture.Snapshot, error) {
	path = f.Path(path)
	document, err := f.store.Canonical(f.ctx, path)
	if err != nil {
		return capture.Snapshot{}, err
	}
	f.mu.Lock()
	f.content[document] = bytes.Clone(content)
	f.mu.Unlock()
	snapshot, err := f.store.Read(f.ctx, path, coordinates)
	if err != nil {
		return capture.Snapshot{}, err
	}
	if err := f.store.Verify(snapshot, content); err != nil {
		return capture.Snapshot{}, err
	}
	return snapshot, nil
}

// CloseCaptures joins fixture IO without inheriting the test's cleanup cancellation.
func CloseCaptures(t testing.TB, store *capture.Store) {
	t.Helper()
	// t.Context is canceled before cleanup runs; keep its values, drop its cancellation.
	parent := context.WithoutCancel(t.Context())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
}
