package builtin_test

import (
	"io"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

func newCapturedWorkflowCache(t testing.TB, root, cwd string, debug io.Writer) *calls.Cache {
	t.Helper()
	cache, _ := newCapturedWorkflowCacheAndSnapshots(t, root, cwd, debug)
	return cache
}

func newCapturedWorkflowCacheAndSnapshots(t testing.TB, root, cwd string, debug io.Writer) (*calls.Cache, *capturedSnapshots) {
	t.Helper()
	cache := calls.NewCache(root, cwd, debug)
	store, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	cache.SetSources(t.Context(), store, sourceFixtureBase(t))
	return cache, snapshots
}

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the capture store for caches under test and
// the fixture that admits documents into the same store.
func newCapturedSnapshots(t testing.TB, base string) (*capture.Store, *capturedSnapshots) {
	t.Helper()
	_, store, snapshots := declarations.NewCapturedSnapshots(t, base)
	return store, snapshots
}
