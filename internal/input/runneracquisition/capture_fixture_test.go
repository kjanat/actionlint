package runneracquisition

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

type capturedSnapshots = declarations.CapturedSnapshots

func newCapturedSnapshots(tb testing.TB, base string) *capturedSnapshots {
	tb.Helper()
	_, _, snapshots := declarations.NewCapturedSnapshots(tb, base)
	return snapshots
}
