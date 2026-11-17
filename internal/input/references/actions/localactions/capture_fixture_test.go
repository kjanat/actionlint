package localactions

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/capture"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedActionCache(tb testing.TB, root string, debug io.Writer, overlays ...capture.Overlay) *Cache {
	tb.Helper()
	cache := NewCache(tb.Context(), root, debug)
	bindCapturedActionSources(tb, cache, overlays...)
	return cache
}

func bindCapturedActionSources(tb testing.TB, cache *Cache, overlays ...capture.Overlay) {
	tb.Helper()
	store, err := capture.New(tb.Context(), overlays, func(_ context.Context, path string) ([]byte, error) {
		return cache.reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		tb.Fatal(err)
	}
	explicit := make(map[string]struct{}, len(overlays))
	for _, overlay := range overlays {
		document, err := store.Canonical(tb.Context(), overlay.Path)
		if err != nil {
			tb.Fatal(err)
		}
		explicit[document] = struct{}{}
	}
	cache.Configure(Options{Captures: store, Context: tb.Context(), WorkingDir: sourceFixtureBase(tb), ExplicitDocuments: explicit})
	capturefixture.CloseCaptures(tb, store)
}

func newCapturedSnapshots(tb testing.TB, base string) *declarations.CapturedSnapshots {
	tb.Helper()
	_, _, snapshots := declarations.NewCapturedSnapshots(tb, base)
	return snapshots
}
