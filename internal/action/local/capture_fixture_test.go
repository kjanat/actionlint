package local

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	testutil "actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
)

func sourceFixtureBase(t testing.TB) string { return testutil.CurrentDir(t) }

func newCapturedActionCache(t testing.TB, root string, debug io.Writer, overlays ...capture.Overlay) *Cache {
	t.Helper()
	cache := NewCache(root, debug)
	bindCapturedActionSources(t, cache, overlays...)
	return cache
}

func bindCapturedActionSources(t testing.TB, cache *Cache, overlays ...capture.Overlay) {
	t.Helper()
	store, err := capture.New(t.Context(), overlays, func(_ context.Context, path string) ([]byte, error) {
		return cache.reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	explicit := make(map[string]struct{}, len(overlays))
	for _, overlay := range overlays {
		document, err := store.Canonical(t.Context(), overlay.Path)
		if err != nil {
			t.Fatal(err)
		}
		explicit[document] = struct{}{}
	}
	cache.Configure(Options{Captures: store, Context: t.Context(), WorkingDir: sourceFixtureBase(t), ExplicitDocuments: explicit})
	testutil.CloseCaptures(t, store)
}

type capturedSnapshots = declarations.CapturedSnapshots

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
