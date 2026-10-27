package execution

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

type capturedSnapshots = declarations.CapturedSnapshots

func newCapturedSnapshots(t testing.TB, base string) *capturedSnapshots {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
