package lsp

import (
	"context"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
)

type Document struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// Analysis Sources and Diagnostics must belong to the same analysis generation.
type Analysis struct {
	Diagnostics []results.Diagnostic
	Sources     map[string]Source
}

type Source struct {
	Text        string
	Revision    sourcetext.Revision
	Coordinates sourcetext.Coordinates
}

func (s Source) valid() bool {
	return s.Coordinates == sourcetext.YAML && s.Revision.Valid() && utf8.ValidString(s.Text) && s.Revision.Digest() == sha256hex.Digest([]byte(s.Text))
}

type (
	Analyze        func(context.Context, map[string]Document) (Analysis, error)
	analysisResult struct {
		generation int
		analysis   Analysis
		err        error
	}
)
