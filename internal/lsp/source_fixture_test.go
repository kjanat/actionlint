package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
)

func testSource(t testing.TB, path, text string) Source {
	t.Helper()
	revision, err := source.NewRevision(sourceid.Document(path), sourceid.Digest([]byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	return Source{Text: text, Revision: revision, Coordinates: source.YAML}
}
