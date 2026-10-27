package builtin

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
)

func newCapturedLocalCache(t testing.TB, root string, debug io.Writer, readers ...*fsread.Reader) *actionlocal.Cache {
	t.Helper()
	reader := fsread.New(t.Context(), nil, nil)
	if len(readers) != 0 {
		reader = readers[0]
	}
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := actionlocal.NewCache(root, debug)
	cache.Configure(actionlocal.Options{Captures: store, Context: t.Context(), WorkingDir: root, Reader: reader, CustomReader: len(readers) != 0})
	capturefixture.CloseCaptures(t, store)
	return cache
}
