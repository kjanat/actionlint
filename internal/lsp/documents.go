package lsp

import (
	"context"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
)

type Document struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// Analysis supplies the captured source text used for diagnostic coordinates.
// Sources and Diagnostics must belong to the same analysis generation.
type Analysis struct {
	Diagnostics []results.Diagnostic
	Sources     map[string]Source
}

// Source pairs captured text with its revision and coordinate model.
type Source struct {
	Text        string
	Revision    sourcetext.Revision
	Coordinates sourcetext.Coordinates
}

func (s Source) valid() bool {
	return s.Coordinates == sourcetext.YAML && s.Revision.Valid() && utf8.ValidString(s.Text) && s.Revision.Digest() == sourcetext.ContentDigest([]byte(s.Text))
}

type (
	Analyze        func(context.Context, map[string]Document) (Analysis, error)
	analysisResult struct {
		generation int
		analysis   Analysis
		err        error
	}
)
