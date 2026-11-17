package component

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
)

type sourceCommentsKey struct{}

func suppliedComments(input checkinputs.AnalyzerInput) map[sourcetext.Reference][]facts.SourceComment {
	comments := make(map[sourcetext.Reference][]facts.SourceComment)
	add := func(script checkinputs.Script) {
		if script.Reference != (sourcetext.Reference{}) {
			comments[script.Reference] = slices.Clone(script.Comments)
		}
	}
	for _, script := range input.Scripts {
		add(script)
	}
	for _, script := range input.Invoked {
		add(script)
	}
	for _, input := range input.ActionInputs {
		add(input.Script)
	}
	return comments
}

type sourceAnnotations struct{}

func (sourceAnnotations) Comments(ctx context.Context, reference invocationcontext.SourceReference) ([]sourceannotations.Comment, error) {
	comments, _ := ctx.Value(sourceCommentsKey{}).(map[sourcetext.Reference][]facts.SourceComment)
	key := sourcetext.Reference{Document: reference.Document, Revision: reference.Revision, Node: reference.Node, Address: reference.Address}
	var result []sourceannotations.Comment
	for _, comment := range comments[key] {
		if !strings.HasPrefix(comment.Text, "#") || strings.ContainsAny(comment.Text, "\r\n") || comment.Start.Line < 1 || comment.Start.Column < 1 {
			return nil, fmt.Errorf("invalid physical source comment at %d:%d", comment.Start.Line, comment.Start.Column)
		}
		var placement sourceannotations.Placement
		switch comment.Placement {
		case facts.LeadingComment:
			placement = sourceannotations.PlacementLeading
		case facts.TrailingComment:
			placement = sourceannotations.PlacementTrailing
		default:
			return nil, fmt.Errorf("invalid source comment placement %d", comment.Placement)
		}
		result = append(result, sourceannotations.Comment{
			Text: comment.Text, Placement: placement,
			Start: invocationcontext.Position{Line: uint32(lowerU32(comment.Start.Line)), Column: uint32(lowerU32(comment.Start.Column))},
		})
	}
	return result, nil
}
