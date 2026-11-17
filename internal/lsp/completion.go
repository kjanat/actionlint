package lsp

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/lsp/completion"
)

// Complete returns property edits in document byte offsets for the cursor
// offset. completionItemFor converts those offsets to UTF-16 positions.
type Complete func(context.Context, Document, int) []completion.Item

type completionItem struct {
	Label    string         `json:"label"`
	Detail   string         `json:"detail"`
	Kind     int            `json:"kind"`
	TextEdit completionEdit `json:"textEdit"`
}
type completionEdit struct {
	Range   editorRange `json:"range"`
	NewText string      `json:"newText"`
}

func completionItemFor(text string, c completion.Item) completionItem {
	content := []byte(text)
	start := editorPositionAtByteOffset(content, max(0, min(c.Start, len(content))))
	end := editorPositionAtByteOffset(content, max(0, min(c.End, len(content))))
	return completionItem{Label: c.Label, Detail: c.Detail, Kind: 10, TextEdit: completionEdit{Range: editorRange{start, end}, NewText: c.Label}}
}
