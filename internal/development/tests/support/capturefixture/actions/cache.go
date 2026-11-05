// Package actions creates action metadata caches backed by captured test sources.
package actions

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	actionlocal "actionlint.kjanat.dev/v2/internal/model/actions/local"
)

// NewCache uses one source-capture store for metadata lookups and closes it when the test ends.
// If a reader is supplied, lookups use it instead of the native filesystem reader.
func NewCache(t testing.TB, root string, debug io.Writer, readers ...*files.Reader) *actionlocal.Cache {
	t.Helper()
	reader := files.New(t.Context(), nil, nil)
	if len(readers) != 0 {
		reader = readers[0]
	}
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := actionlocal.NewCache(t.Context(), root, debug)
	cache.Configure(actionlocal.Options{Captures: store, Context: t.Context(), WorkingDir: root, Reader: reader, CustomReader: len(readers) != 0})
	capturefixture.CloseCaptures(t, store)
	return cache
}
