package langserver

import (
	"actionlint.kjanat.dev/diagnostic"
	"context"
	"unicode/utf8"
)

// Completion is a buffer-relative property edit; the transport owns UTF-16 conversion.
type Completion struct {
	Label, Detail string
	Start, End    int
}
type Complete func(context.Context, Document, int) []Completion

type completionItem struct {
	Label    string         `json:"label"`
	Detail   string         `json:"detail"`
	Kind     int            `json:"kind"`
	TextEdit completionEdit `json:"textEdit"`
}
type completionEdit struct {
	Range   sourceRange `json:"range"`
	NewText string      `json:"newText"`
}

func completionItemFor(text string, c Completion) completionItem {
	return completionItem{Label: c.Label, Detail: c.Detail, Kind: 10, TextEdit: completionEdit{Range: sourceRange{positionAtOffset(text, c.Start), positionAtOffset(text, c.End)}, NewText: c.Label}}
}
func positionAtOffset(text string, offset int) position {
	offset = max(0, min(offset, len(text)))
	line, column := 1, 1
	for at := 0; at < offset; {
		r, n := utf8.DecodeRuneInString(text[at:])
		at += n
		if r == '\r' && at < len(text) && text[at] == '\n' {
			at++
		}
		if r == '\n' || r == '\r' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return editorPosition([]byte(text), diagnostic.Position{Line: line, Column: column})
}
