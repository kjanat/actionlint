package scripts

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the registry and capture store for code under
// test and the fixture that admits documents into them.
func newCapturedSnapshots(t testing.TB, base string) (*parsing.SourceSnapshots, *capture.Store, *capturedSnapshots) {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
