package execution

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture/declarations"
)

func sourceFixtureBase(t testing.TB) string { return capturefixture.CurrentDir(t) }

type capturedSnapshots = declarations.CapturedSnapshots

// newCapturedSnapshots returns the registry and capture store for code under
// test and the fixture that admits documents into them.
func newCapturedSnapshots(t testing.TB, base string) (*documentparse.SourceSnapshots, *capture.Store, *capturedSnapshots) {
	t.Helper()
	return declarations.NewCapturedSnapshots(t, base)
}
