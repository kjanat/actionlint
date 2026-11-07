package reportfixture

import (
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// CapturedSource binds test content to its source revision and coordinate system.
func CapturedSource(tb testing.TB, path string, content []byte, model source.Coordinates) report.CapturedSource {
	tb.Helper()
	revision, err := source.NewRevision(sourceid.Document(path), sourceid.Digest(content))
	if err != nil {
		tb.Fatal(err)
	}
	return report.CapturedSource{Revision: revision, Coordinates: model, CoordinatesAvailable: utf8.Valid(content), Content: content}
}
