package lsp

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/lsp/completion"
)

// CompleteDocument adapts shared editor completions to the language server.
func CompleteDocument(ctx context.Context, doc Document, offset int) []completion.Item {
	if ctx.Err() != nil {
		return nil
	}
	return completion.Complete(doc.Text, offset)
}
