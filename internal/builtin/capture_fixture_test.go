package builtin_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

func newCapturedWorkflowCache(t testing.TB, root, cwd string, debug io.Writer) *calls.Cache {
	t.Helper()
	cache := calls.NewCache(root, cwd, debug)
	store, err := capture.New(t.Context(), nil, func(_ context.Context, path string) ([]byte, error) {
		return os.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		t.Fatal(err)
	}
	cache.SetSources(t.Context(), store, sourceFixtureBase(t))
	capturefixture.CloseCaptures(t, store)
	return cache
}

type capturedSnapshots = declarations.CapturedSnapshots

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
