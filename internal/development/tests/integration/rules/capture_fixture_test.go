package rules_test

import (
	"io"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *reusableworkflows.MetadataCache {
	tb.Helper()
	cache, _ := newCapturedWorkflowCacheAndSnapshots(tb, root, cwd, debug)
	return cache
}

func newCapturedWorkflowCacheAndSnapshots(tb testing.TB, root, cwd string, debug io.Writer) (*reusableworkflows.MetadataCache, *declarations.CapturedSnapshots) {
	tb.Helper()
	cache := reusableworkflows.NewMetadataCache(root, debug)
	store, snapshots := newCapturedSnapshots(tb, sourceFixtureBase(tb))
	cache.SetSources(tb.Context(), store, sourceFixtureBase(tb))
	return cache, snapshots
}

func newCapturedSnapshots(tb testing.TB, base string) (*capture.Store, *declarations.CapturedSnapshots) {
	tb.Helper()
	_, store, snapshots := declarations.NewCapturedSnapshots(tb, base)
	return store, snapshots
}
