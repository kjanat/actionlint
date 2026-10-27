package calls

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
)

func newCapturedWorkflowCache(t testing.TB, root, cwd string, debug io.Writer) *Cache {
	t.Helper()
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCache(root, cwd, debug)
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return os.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	cache.SetSources(t.Context(), store, base)
	capturefixture.CloseCaptures(t, store)
	return cache
}
