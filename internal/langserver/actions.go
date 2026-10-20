package langserver

import (
	"context"
	"errors"
	"maps"
	"slices"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/fix"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/sourceid"
)

func codeActions(ctx context.Context, path string, selection sourceRange, findings []diagnostic.Diagnostic, documents map[string]Document) []any {
	actions := []any{}
	for _, d := range findings {
		doc := documents[path]
		location := diagnosticRange([]byte(doc.Text), d.Start, d.End)
		if comparePosition(location.End, selection.Start) < 0 || comparePosition(location.Start, selection.End) > 0 {
			continue
		}
		for _, f := range d.Fixes {
			_, validationErr := fix.Build(ctx, []diagnostic.Diagnostic{{Fixes: []diagnostic.Fix{f}}}, fsread.New(ctx, func(p string) ([]byte, error) {
				doc, ok := documents[p]
				if !ok {
					return nil, errors.New("document is not open")
				}
				return []byte(doc.Text), nil
			}, nil), true)
			if validationErr != nil {
				continue
			}
			changes := map[string][]any{}
			valid := len(f.Edits) > 0
			for _, e := range f.Edits {
				target, ok := documents[e.Path]
				if !ok || f.Revisions[e.Path] != sourceid.Digest([]byte(target.Text)) {
					valid = false
					break
				}
				changes[e.Path] = append(changes[e.Path], map[string]any{"range": sourceRange{editorPosition([]byte(target.Text), e.Start), editorPosition([]byte(target.Text), e.End)}, "newText": e.Replacement})
			}
			if !valid {
				continue
			}
			edits := make([]any, 0, len(changes))
			for _, target := range slices.Sorted(maps.Keys(changes)) {
				doc := documents[target]
				edits = append(edits, map[string]any{"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version}, "edits": changes[target]})
			}
			actions = append(actions, map[string]any{"title": f.Description, "kind": "quickfix", "isPreferred": f.Applicability == "safe", "edit": map[string]any{"documentChanges": edits}})
		}
	}
	return actions
}
