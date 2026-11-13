package component

import (
	"context"
	"reflect"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	invocationbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
)

func TestSourceAnnotationsRestrictReferencesAndCallLifetime(t *testing.T) {
	t.Parallel()
	root := sourcepos.Reference{Document: "ci.yml", Revision: "root", Node: "step1", Address: "jobs.test.steps[0]"}
	invoked := sourcepos.Reference{Document: "action.yml", Revision: "action", Node: "step1", Address: "runs.steps[0]"}
	actionInput := sourcepos.Reference{Document: "ci.yml", Revision: "root", Node: "input1", Address: "jobs.test.steps[1].with.script"}
	inputScript := checkinputs.Script{Reference: actionInput, Comments: []workflowfacts.SourceComment{{Text: "# action input", Start: sourcepos.Position{Line: 12, Column: 7}}}}
	input := checkinputs.Input{
		Scripts:      []checkinputs.Script{{Reference: root, Comments: []workflowfacts.SourceComment{{Text: "# root", Start: sourcepos.Position{Line: 1, Column: 3}}}}},
		Invoked:      []checkinputs.Script{{Reference: invoked, Comments: []workflowfacts.SourceComment{{Text: "# invoked", Start: sourcepos.Position{Line: 9, Column: 5}, Placement: workflowfacts.TrailingComment}}}},
		ActionInputs: []checkinputs.ActionInput{{Script: inputScript}},
	}
	query := func(ctx context.Context, ref sourcepos.Reference) []annotationsbind.Comment {
		t.Helper()
		comments, err := (sourceAnnotations{}).Comments(ctx, invocationbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
		if err != nil {
			t.Fatal(err)
		}
		return comments
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	input.Scripts[0].Comments[0].Text = "# mutation"
	input.Invoked[0].Comments[0].Start.Line = 99
	input.ActionInputs[0].Comments[0].Placement = workflowfacts.TrailingComment
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
	for _, forged := range []sourcepos.Reference{
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
		ref sourcepos.Reference
	}{{ctx, sourcepos.Reference{}}, {t.Context(), root}, {context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(checkinputs.Input{})), root}} {
		if got := query(scope.ctx, scope.ref); len(got) != 0 {
			t.Fatalf("unauthorized comments=%+v", got)
		}
	}
}

func TestSourceAnnotationsDoNotGrantUnidentifiedScripts(t *testing.T) {
	t.Parallel()
	comment := workflowfacts.SourceComment{Text: "# unidentified", Start: sourcepos.Position{Line: 1, Column: 1}}
	unidentified := checkinputs.Script{Comments: []workflowfacts.SourceComment{comment}}
	input := checkinputs.Input{
		Scripts:      []checkinputs.Script{unidentified},
		Invoked:      []checkinputs.Script{unidentified},
		ActionInputs: []checkinputs.ActionInput{{Script: unidentified}},
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, invocationbind.SourceReference{})
	if err != nil || len(got) != 0 {
		t.Fatalf("unidentified scripts granted comments=%+v, err=%v", got, err)
	}
}

func TestSourceAnnotationsValidatePhysicalComments(t *testing.T) {
	t.Parallel()
	ref := sourcepos.Reference{Document: "ci.yml", Revision: "root", Node: "step", Address: "jobs.test.steps[0]"}
	valid := workflowfacts.SourceComment{Text: "# shellcheck disable=SC2086", Start: sourcepos.Position{Line: 3, Column: 7}}
	for _, test := range []struct {
		name, text string
		start      sourcepos.Position
		placement  workflowfacts.CommentPlacement
		want       string
	}{
		{"empty", "", valid.Start, workflowfacts.LeadingComment, "invalid physical source comment at 3:7"},
		{"missing marker", "shellcheck disable=SC2086", valid.Start, workflowfacts.LeadingComment, "invalid physical source comment at 3:7"},
		{"line feed", "# first\n# second", valid.Start, workflowfacts.LeadingComment, "invalid physical source comment at 3:7"},
		{"carriage return", "# first\r# second", valid.Start, workflowfacts.LeadingComment, "invalid physical source comment at 3:7"},
		{"zero line", valid.Text, sourcepos.Position{Line: 0, Column: 7}, workflowfacts.LeadingComment, "invalid physical source comment at 0:7"},
		{"negative line", valid.Text, sourcepos.Position{Line: -1, Column: 7}, workflowfacts.LeadingComment, "invalid physical source comment at -1:7"},
		{"zero column", valid.Text, sourcepos.Position{Line: 3, Column: 0}, workflowfacts.LeadingComment, "invalid physical source comment at 3:0"},
		{"negative column", valid.Text, sourcepos.Position{Line: 3, Column: -1}, workflowfacts.LeadingComment, "invalid physical source comment at 3:-1"},
		{"unknown placement", valid.Text, valid.Start, workflowfacts.CommentPlacement(2), "invalid source comment placement 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.Input{Scripts: []checkinputs.Script{{
				Reference: ref,
				Comments:  []workflowfacts.SourceComment{valid, {Text: test.text, Start: test.start, Placement: test.placement}},
			}}}
			ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
			got, err := (sourceAnnotations{}).Comments(ctx, invocationbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
			if got != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid comment leaked a partial result: comments=%+v, err=%v", got, err)
			}
		})
	}
	input := checkinputs.Input{Scripts: []checkinputs.Script{{Reference: ref, Comments: []workflowfacts.SourceComment{valid, {Text: "#", Start: sourcepos.Position{Line: 4, Column: 1}, Placement: workflowfacts.TrailingComment}}}}}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, invocationbind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
	want := []annotationsbind.Comment{
		{Text: valid.Text, Start: invocationbind.Position{Line: 3, Column: 7}, Placement: annotationsbind.PlacementLeading},
		{Text: "#", Start: invocationbind.Position{Line: 4, Column: 1}, Placement: annotationsbind.PlacementTrailing},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("valid physical comments=%+v, err=%v, want=%+v", got, err, want)
	}
}
