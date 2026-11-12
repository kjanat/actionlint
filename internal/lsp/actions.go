package lsp

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/files"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
	sourceedits "actionlint.kjanat.dev/v2/internal/results/edits"
)

func codeActions(ctx context.Context, path string, selection sourceRange, findings []results.Diagnostic, sources, documents map[string]Document) []any {
	actions := []any{}
	var offered []results.Fix
	for _, d := range findings {
		doc, captured := sources[path]
		if !captured {
			continue
		}
		location, err := diagnosticRange([]byte(doc.Text), d.Start, d.End)
		if err != nil {
			continue
		}
		if comparePosition(location.End, selection.Start) < 0 || comparePosition(location.Start, selection.End) > 0 {
			continue
		}
		for _, f := range d.Fixes {
			if slices.ContainsFunc(offered, func(previous results.Fix) bool { return reflect.DeepEqual(previous, f) }) {
				continue
			}
			_, validationErr := sourceedits.Build(ctx, []results.Diagnostic{{Fixes: []results.Fix{f}}}, files.New(ctx, func(p string) ([]byte, error) {
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
				rangeValue, err := diagnosticRange([]byte(target.Text), e.Start, e.End)
				if err != nil {
					valid = false
					break
				}
				changes[e.Path] = append(changes[e.Path], map[string]any{"range": rangeValue, "newText": e.Replacement})
			}
			if !valid {
				continue
			}
			edits := make([]any, 0, len(changes))
			for _, target := range slices.Sorted(maps.Keys(changes)) {
				doc := documents[target]
				edits = append(edits, map[string]any{"textDocument": map[string]any{"uri": doc.URI, "version": doc.Version}, "edits": changes[target]})
			}
			actions = append(actions, map[string]any{"title": f.Description, "kind": "quickfix", "isPreferred": f.Applicability.Automatic(), "diagnostics": diagnosticItems(doc, []results.Diagnostic{d}, sources), "edit": map[string]any{"documentChanges": edits}})
			offered = append(offered, f)
		}
	}
	return actions
}
