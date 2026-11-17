package component

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
)

func TestSourceAnnotationsRestrictReferencesAndCallLifetime(t *testing.T) {
	t.Parallel()
	root := sourcetext.Reference{Document: "ci.yml", Revision: "root", Node: "step1", Address: "jobs.test.steps[0]"}
	invoked := sourcetext.Reference{Document: "action.yml", Revision: "action", Node: "step1", Address: "runs.steps[0]"}
	actionInput := sourcetext.Reference{Document: "ci.yml", Revision: "root", Node: "input1", Address: "jobs.test.steps[1].with.script"}
	inputScript := checkinputs.Script{Reference: actionInput, Comments: []facts.SourceComment{{Text: "# action input", Start: sourcetext.Position{Line: 12, Column: 7}}}}
	input := checkinputs.AnalyzerInput{
		Scripts:      []checkinputs.Script{{Reference: root, Comments: []facts.SourceComment{{Text: "# root", Start: sourcetext.Position{Line: 1, Column: 3}}}}},
		Invoked:      []checkinputs.Script{{Reference: invoked, Comments: []facts.SourceComment{{Text: "# invoked", Start: sourcetext.Position{Line: 9, Column: 5}, Placement: facts.TrailingComment}}}},
		ActionInputs: []checkinputs.ActionInput{{Script: inputScript}},
	}
	query := func(ctx context.Context, ref sourcetext.Reference) []sourceannotations.Comment {
		t.Helper()
		comments, err := (sourceAnnotations{}).Comments(ctx, invocationcontext.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
		if err != nil {
			t.Fatal(err)
		}
		return comments
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	input.Scripts[0].Comments[0].Text = "# mutation"
	input.Invoked[0].Comments[0].Start.Line = 99
	input.ActionInputs[0].Comments[0].Placement = facts.TrailingComment
	if got := query(ctx, root); len(got) != 1 || got[0].Text != "# root" || got[0].Start.Column != 3 {
		t.Fatalf("snapshot root comments=%+v", got)
	}
	if got := query(ctx, invoked); len(got) != 1 || got[0].Text != "# invoked" || got[0].Placement != sourceannotations.PlacementTrailing || got[0].Start.Line != 9 {
		t.Fatalf("invoked comments=%+v", got)
	}
	if got := query(ctx, actionInput); len(got) != 1 || got[0].Text != "# action input" || got[0].Placement != sourceannotations.PlacementLeading || got[0].Start.Line != 12 {
		t.Fatalf("action input comments=%+v", got)
	}
	// Returning a result must not give the caller ownership of the stored slice.
	query(ctx, root)[0].Text = "# returned mutation"
	if got := query(ctx, root); len(got) != 1 || got[0].Text != "# root" {
		t.Fatalf("returned comments alias the snapshot: %+v", got)
	}
	for _, forged := range []sourcetext.Reference{
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
		ref sourcetext.Reference
	}{{ctx, sourcetext.Reference{}}, {t.Context(), root}, {context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(checkinputs.AnalyzerInput{})), root}} {
		if got := query(scope.ctx, scope.ref); len(got) != 0 {
			t.Fatalf("unauthorized comments=%+v", got)
		}
	}
}

func TestSourceAnnotationsDoNotGrantUnidentifiedScripts(t *testing.T) {
	t.Parallel()
	comment := facts.SourceComment{Text: "# unidentified", Start: sourcetext.Position{Line: 1, Column: 1}}
	unidentified := checkinputs.Script{Comments: []facts.SourceComment{comment}}
	input := checkinputs.AnalyzerInput{
		Scripts:      []checkinputs.Script{unidentified},
		Invoked:      []checkinputs.Script{unidentified},
		ActionInputs: []checkinputs.ActionInput{{Script: unidentified}},
	}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, invocationcontext.SourceReference{})
	if err != nil || len(got) != 0 {
		t.Fatalf("unidentified scripts granted comments=%+v, err=%v", got, err)
	}
}

func TestSourceAnnotationsValidatePhysicalComments(t *testing.T) {
	t.Parallel()
	ref := sourcetext.Reference{Document: "ci.yml", Revision: "root", Node: "step", Address: "jobs.test.steps[0]"}
	valid := facts.SourceComment{Text: "# shellcheck disable=SC2086", Start: sourcetext.Position{Line: 3, Column: 7}}
	for _, test := range []struct {
		name, text string
		start      sourcetext.Position
		placement  facts.CommentPlacement
		want       string
	}{
		{"empty", "", valid.Start, facts.LeadingComment, "invalid physical source comment at 3:7"},
		{"missing marker", "shellcheck disable=SC2086", valid.Start, facts.LeadingComment, "invalid physical source comment at 3:7"},
		{"line feed", "# first\n# second", valid.Start, facts.LeadingComment, "invalid physical source comment at 3:7"},
		{"carriage return", "# first\r# second", valid.Start, facts.LeadingComment, "invalid physical source comment at 3:7"},
		{"zero line", valid.Text, sourcetext.Position{Line: 0, Column: 7}, facts.LeadingComment, "invalid physical source comment at 0:7"},
		{"negative line", valid.Text, sourcetext.Position{Line: -1, Column: 7}, facts.LeadingComment, "invalid physical source comment at -1:7"},
		{"zero column", valid.Text, sourcetext.Position{Line: 3, Column: 0}, facts.LeadingComment, "invalid physical source comment at 3:0"},
		{"negative column", valid.Text, sourcetext.Position{Line: 3, Column: -1}, facts.LeadingComment, "invalid physical source comment at 3:-1"},
		{"unknown placement", valid.Text, valid.Start, facts.CommentPlacement(2), "invalid source comment placement 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.AnalyzerInput{Scripts: []checkinputs.Script{{
				Reference: ref,
				Comments:  []facts.SourceComment{valid, {Text: test.text, Start: test.start, Placement: test.placement}},
			}}}
			ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
			got, err := (sourceAnnotations{}).Comments(ctx, invocationcontext.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
			if got != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid comment leaked a partial result: comments=%+v, err=%v", got, err)
			}
		})
	}
	input := checkinputs.AnalyzerInput{Scripts: []checkinputs.Script{{Reference: ref, Comments: []facts.SourceComment{valid, {Text: "#", Start: sourcetext.Position{Line: 4, Column: 1}, Placement: facts.TrailingComment}}}}}
	ctx := context.WithValue(t.Context(), sourceCommentsKey{}, suppliedComments(input))
	got, err := (sourceAnnotations{}).Comments(ctx, invocationcontext.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address})
	want := []sourceannotations.Comment{
		{Text: valid.Text, Start: invocationcontext.Position{Line: 3, Column: 7}, Placement: sourceannotations.PlacementLeading},
		{Text: "#", Start: invocationcontext.Position{Line: 4, Column: 1}, Placement: sourceannotations.PlacementTrailing},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("valid physical comments=%+v, err=%v, want=%+v", got, err, want)
	}
}
