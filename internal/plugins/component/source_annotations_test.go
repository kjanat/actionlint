package component

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceAnnotationsRestrictReferencesAndCallLifetime(t *testing.T) {
	root := source.Reference{Document: "ci.yml", Revision: "root", Node: "step1", Address: "jobs.test.steps[0]"}
	invoked := source.Reference{Document: "action.yml", Revision: "action", Node: "step1", Address: "runs.steps[0]"}
	input := analyzer.Input{
		Scripts: []analyzer.Script{{Reference: root, Comments: []analyzer.SourceComment{{Text: "# root", Start: diagnostic.Position{Line: 1, Column: 3}}}}},
		Invoked: []analyzer.Script{{Reference: invoked, Comments: []analyzer.SourceComment{{Text: "# invoked", Start: diagnostic.Position{Line: 9, Column: 5}, Placement: analyzer.TrailingComment}}}},
	}
	query := func(ctx context.Context, ref source.Reference) []annotationsbind.Comment {
		t.Helper()
		comments, err := (sourceAnnotations{}).Comments(ctx, annotationsbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
		if err != nil {
			t.Fatal(err)
		}
		return comments
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	input.Scripts[0].Comments[0].Text = "# mutation"
	if got := query(ctx, root); len(got) != 1 || got[0].Text != "# root" || got[0].Start.Column != 3 {
		t.Fatalf("snapshot root comments=%+v", got)
	}
	if got := query(ctx, invoked); len(got) != 1 || got[0].Text != "# invoked" || got[0].Placement != annotationsbind.PlacementTrailing || got[0].Start.Line != 9 {
		t.Fatalf("invoked comments=%+v", got)
	}
	unknown := root
	unknown.Revision = "other"
	for _, scope := range []struct {
		ctx context.Context
		ref source.Reference
	}{{ctx, unknown}, {ctx, source.Reference{}}, {t.Context(), root}, {context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(analyzer.Input{})), root}} {
		if got := query(scope.ctx, scope.ref); len(got) != 0 {
			t.Fatalf("unauthorized comments=%+v", got)
		}
	}
}
