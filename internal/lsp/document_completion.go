package lsp

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/lsp/completion"
)

// CompleteDocument adapts shared editor completions to the language server.
func CompleteDocument(ctx context.Context, doc Document, offset int) []Completion {
	if ctx.Err() != nil {
		return nil
	}
	items := completion.Complete(doc.Text, offset)
	result := make([]Completion, 0, len(items))
	for _, item := range items {
		result = append(result, Completion{Label: item.Label, Detail: item.Detail, Start: item.Start, End: item.End})
	}
	return result
}
