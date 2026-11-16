package scriptanalysis

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/capture"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func sourceFixtureBase(tb testing.TB) string {
	tb.Helper()
	return capturefixture.CurrentDir(tb)
}

// newCapturedSnapshots returns the registry and capture store for code under
// test and the fixture that admits documents into them.
func newCapturedSnapshots(tb testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *declarations.CapturedSnapshots) {
	tb.Helper()
	return declarations.NewCapturedSnapshots(tb, base)
}
