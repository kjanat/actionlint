package runners

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

func newCapturedSnapshots(tb testing.TB, base string) *declarations.CapturedSnapshots {
	tb.Helper()
	_, _, snapshots := declarations.NewCapturedSnapshots(tb, base)
	return snapshots
}
