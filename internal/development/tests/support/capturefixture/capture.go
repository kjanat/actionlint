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

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
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
func NewCapturedSources(tb testing.TB, base string, overlays ...capture.Overlay) (*capture.Store, *CapturedSources) {
	tb.Helper()
	if !filepath.IsAbs(base) {
		tb.Fatalf("capture fixture requires an absolute base: %q", base)
	}
	fixture := &CapturedSources{base: base, ctx: tb.Context(), content: make(map[string][]byte)}
	reader := files.New(tb.Context(), nil, nil)
	store, err := capture.New(tb.Context(), overlays, func(_ context.Context, document string) ([]byte, error) {
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
		tb.Fatal(err)
	}
	fixture.store = store
	CloseCaptures(tb, store)
	return store, fixture
}

// CurrentDir returns the absolute working directory for relative-path fixtures.
func CurrentDir(tb testing.TB) string {
	tb.Helper()
	base, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
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
func CloseCaptures(tb testing.TB, store *capture.Store) {
	tb.Helper()
	// Cleanup uses the test context values without inheriting its cancellation.
	parent := context.WithoutCancel(tb.Context())
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			tb.Error(err)
		}
	})
}
