package component

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceAnnotationsRestrictReferencesAndCallLifetime(t *testing.T) {
	root := source.Reference{Document: "ci.yml", Revision: "root", Node: "step1", Address: "jobs.test.steps[0]"}
	invoked := source.Reference{Document: "action.yml", Revision: "action", Node: "step1", Address: "runs.steps[0]"}
	actionInput := source.Reference{Document: "ci.yml", Revision: "root", Node: "input1", Address: "jobs.test.steps[1].with.script"}
	inputScript := analyzer.Script{Reference: actionInput, Comments: []analyzer.SourceComment{{Text: "# action input", Start: diagnostic.Position{Line: 12, Column: 7}}}}
	input := analyzer.Input{
		Scripts:      []analyzer.Script{{Reference: root, Comments: []analyzer.SourceComment{{Text: "# root", Start: diagnostic.Position{Line: 1, Column: 3}}}}},
		Invoked:      []analyzer.Script{{Reference: invoked, Comments: []analyzer.SourceComment{{Text: "# invoked", Start: diagnostic.Position{Line: 9, Column: 5}, Placement: analyzer.TrailingComment}}}},
		ActionInputs: []analyzer.ActionInput{{Script: inputScript}},
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
	input.Invoked[0].Comments[0].Start.Line = 99
	input.ActionInputs[0].Comments[0].Placement = analyzer.TrailingComment
	if got := query(ctx, root); len(got) != 1 || got[0].Text != "# root" || got[0].Start.Column != 3 {
		t.Fatalf("snapshot root comments=%+v", got)
	}
	if got := query(ctx, invoked); len(got) != 1 || got[0].Text != "# invoked" || got[0].Placement != annotationsbind.PlacementTrailing || got[0].Start.Line != 9 {
		t.Fatalf("invoked comments=%+v", got)
	}
	if got := query(ctx, actionInput); len(got) != 1 || got[0].Text != "# action input" || got[0].Placement != annotationsbind.PlacementLeading || got[0].Start.Line != 12 {
		t.Fatalf("action input comments=%+v", got)
	}
	// Returning a result must not give the caller ownership of the stored slice.
	query(ctx, root)[0].Text = "# returned mutation"
	if got := query(ctx, root); len(got) != 1 || got[0].Text != "# root" {
		t.Fatalf("returned comments alias the snapshot: %+v", got)
	}
	for _, forged := range []source.Reference{
		{Document: "other.yml", Revision: root.Revision, Node: root.Node, Address: root.Address},
		{Document: root.Document, Revision: "other", Node: root.Node, Address: root.Address},
		{Document: root.Document, Revision: root.Revision, Node: "other", Address: root.Address},
		{Document: root.Document, Revision: root.Revision, Node: root.Node, Address: "jobs.test.steps[1]"},
	} {
		if got := query(ctx, forged); len(got) != 0 {
			t.Fatalf("forged reference %+v exposed comments=%+v", forged, got)
		}
	}
	for _, scope := range []struct {
		ctx context.Context
		ref source.Reference
	}{{ctx, source.Reference{}}, {t.Context(), root}, {context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(analyzer.Input{})), root}} {
		if got := query(scope.ctx, scope.ref); len(got) != 0 {
			t.Fatalf("unauthorized comments=%+v", got)
		}
	}
}

func TestSourceAnnotationsDoNotGrantUnidentifiedScripts(t *testing.T) {
	comment := analyzer.SourceComment{Text: "# unidentified", Start: diagnostic.Position{Line: 1, Column: 1}}
	unidentified := analyzer.Script{Comments: []analyzer.SourceComment{comment}}
	input := analyzer.Input{
		Scripts:      []analyzer.Script{unidentified},
		Invoked:      []analyzer.Script{unidentified},
		ActionInputs: []analyzer.ActionInput{{Script: unidentified}},
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, annotationsbind.SourceReference{})
	if err != nil || len(got) != 0 {
		t.Fatalf("unidentified scripts granted comments=%+v, err=%v", got, err)
	}
}

func TestSourceAnnotationsValidatePhysicalComments(t *testing.T) {
	ref := source.Reference{Document: "ci.yml", Revision: "root", Node: "step", Address: "jobs.test.steps[0]"}
	valid := analyzer.SourceComment{Text: "# shellcheck disable=SC2086", Start: diagnostic.Position{Line: 3, Column: 7}}
	for _, test := range []struct {
		name, text string
		start      diagnostic.Position
		placement  analyzer.CommentPlacement
		want       string
	}{
		{"empty", "", valid.Start, analyzer.LeadingComment, "invalid physical source comment at 3:7"},
		{"missing marker", "shellcheck disable=SC2086", valid.Start, analyzer.LeadingComment, "invalid physical source comment at 3:7"},
		{"line feed", "# first\n# second", valid.Start, analyzer.LeadingComment, "invalid physical source comment at 3:7"},
		{"carriage return", "# first\r# second", valid.Start, analyzer.LeadingComment, "invalid physical source comment at 3:7"},
		{"zero line", valid.Text, diagnostic.Position{Line: 0, Column: 7}, analyzer.LeadingComment, "invalid physical source comment at 0:7"},
		{"negative line", valid.Text, diagnostic.Position{Line: -1, Column: 7}, analyzer.LeadingComment, "invalid physical source comment at -1:7"},
		{"zero column", valid.Text, diagnostic.Position{Line: 3, Column: 0}, analyzer.LeadingComment, "invalid physical source comment at 3:0"},
		{"negative column", valid.Text, diagnostic.Position{Line: 3, Column: -1}, analyzer.LeadingComment, "invalid physical source comment at 3:-1"},
		{"unknown placement", valid.Text, valid.Start, analyzer.CommentPlacement(2), "invalid source comment placement 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := analyzer.Input{Scripts: []analyzer.Script{{
				Reference: ref,
				Comments:  []analyzer.SourceComment{valid, {Text: test.text, Start: test.start, Placement: test.placement}},
			}}}
			ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
			got, err := (sourceAnnotations{}).Comments(ctx, annotationsbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
			if got != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid comment leaked a partial result: comments=%+v, err=%v", got, err)
			}
		})
	}
	input := analyzer.Input{Scripts: []analyzer.Script{{Reference: ref, Comments: []analyzer.SourceComment{valid, {Text: "#", Start: diagnostic.Position{Line: 4, Column: 1}, Placement: analyzer.TrailingComment}}}}}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, annotationsbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
	want := []annotationsbind.Comment{
		{Text: valid.Text, Start: annotationsbind.Position{Line: 3, Column: 7}, Placement: annotationsbind.PlacementLeading},
		{Text: "#", Start: annotationsbind.Position{Line: 4, Column: 1}, Placement: annotationsbind.PlacementTrailing},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("valid physical comments=%+v, err=%v, want=%+v", got, err, want)
	}
}
