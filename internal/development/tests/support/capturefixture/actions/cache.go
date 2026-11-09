// Package actions creates action metadata caches backed by captured test sources.
package actions

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/files"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
)

// NewCache uses one source-capture store for metadata lookups and closes it when the test ends.
// Without a supplied reader, lookups use the native filesystem reader.
func NewCache(tb testing.TB, root string, debug io.Writer, readers ...*files.Reader) *workspaceactions.Cache {
	tb.Helper()
	reader := files.New(tb.Context(), nil, nil)
	if len(readers) != 0 {
		reader = readers[0]
	}
	store, err := capture.New(tb.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		tb.Fatal(err)
	}
	cache := workspaceactions.NewCache(tb.Context(), root, debug)
	cache.Configure(workspaceactions.Options{Captures: store, Context: tb.Context(), WorkingDir: root, Reader: reader, CustomReader: len(readers) != 0})
	capturefixture.CloseCaptures(tb, store)
	return cache
}
