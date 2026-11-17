package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func testSource(tb testing.TB, path, text string) Source {
	tb.Helper()
	revision, err := sourcetext.NewRevision(sourcetext.DocumentID(path), sha256hex.Digest([]byte(text)))
	if err != nil {
		tb.Fatal(err)
	}
	return Source{Text: text, Revision: revision, Coordinates: sourcetext.YAML}
}
