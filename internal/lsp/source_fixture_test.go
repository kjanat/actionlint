package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

func testSource(tb testing.TB, path, text string) Source {
	tb.Helper()
	revision, err := sourcetext.NewRevision(sourcetext.DocumentID(path), sourcetext.ContentDigest([]byte(text)))
	if err != nil {
		tb.Fatal(err)
	}
	return Source{Text: text, Revision: revision, Coordinates: sourcetext.YAML}
}
