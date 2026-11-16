package reusableworkflows

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
)

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *ASTComparisonCache {
	tb.Helper()
	base, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	cache := NewMetadataCache(root, debug)
	store, err := capture.New(tb.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return os.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		tb.Fatal(err)
	}
	cache.SetSources(tb.Context(), store, base)
	capturefixture.CloseCaptures(tb, store)
	return &ASTComparisonCache{MetadataCache: cache, WorkingDir: cwd}
}
