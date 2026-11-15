package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
)

func testSource(tb testing.TB, path, text string) Source {
	tb.Helper()
	revision, err := source.NewRevision(source.DocumentID(path), source.ContentDigest([]byte(text)))
	if err != nil {
		tb.Fatal(err)
	}
	return Source{Text: text, Revision: revision, Coordinates: source.YAML}
}
