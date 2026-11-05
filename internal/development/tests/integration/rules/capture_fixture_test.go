package rules_test

import (
	"io"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *calls.Cache {
	tb.Helper()
	cache, _ := newCapturedWorkflowCacheAndSnapshots(tb, root, cwd, debug)
	return cache
}

func newCapturedWorkflowCacheAndSnapshots(tb testing.TB, root, cwd string, debug io.Writer) (*calls.Cache, *capturedSnapshots) {
	tb.Helper()
	cache := calls.NewCache(root, cwd, debug)
	store, snapshots := newCapturedSnapshots(tb, sourceFixtureBase(tb))
	cache.SetSources(tb.Context(), store, sourceFixtureBase(tb))
	return cache, snapshots
}

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the capture store for caches under test and
// the fixture that admits documents into the same store.
func newCapturedSnapshots(tb testing.TB, base string) (*capture.Store, *capturedSnapshots) {
	tb.Helper()
	_, store, snapshots := declarations.NewCapturedSnapshots(tb, base)
	return store, snapshots
}
