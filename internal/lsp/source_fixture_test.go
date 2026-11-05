package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
)

func testSource(tb testing.TB, path, text string) Source {
	tb.Helper()
	revision, err := source.NewRevision(sourceid.Document(path), sourceid.Digest([]byte(text)))
	if err != nil {
		tb.Fatal(err)
	}
	return Source{Text: text, Revision: revision, Coordinates: source.YAML}
}
