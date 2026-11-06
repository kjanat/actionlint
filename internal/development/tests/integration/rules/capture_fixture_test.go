package rules_test

import (
	"io"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *workflowinputs.Cache {
	tb.Helper()
	cache, _ := newCapturedWorkflowCacheAndSnapshots(tb, root, cwd, debug)
	return cache
}

func newCapturedWorkflowCacheAndSnapshots(tb testing.TB, root, cwd string, debug io.Writer) (*workflowinputs.Cache, *capturedSnapshots) {
	tb.Helper()
	cache := workflowinputs.NewCache(root, debug)
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
