package component

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
)

type sourceCommentsKey struct{}

func suppliedComments(input checkinputs.AnalyzerInput) map[source.Reference][]facts.SourceComment {
	comments := make(map[source.Reference][]facts.SourceComment)
	add := func(script checkinputs.Script) {
		if script.Reference != (source.Reference{}) {
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

func (sourceAnnotations) Comments(ctx context.Context, reference invocationcontext.SourceReference) ([]annotationsbind.Comment, error) {
	comments, _ := ctx.Value(sourceCommentsKey{}).(map[source.Reference][]facts.SourceComment)
	key := source.Reference{Document: reference.Document, Revision: reference.Revision, Node: reference.Node, Address: reference.Address}
	var result []annotationsbind.Comment
	for _, comment := range comments[key] {
		if !strings.HasPrefix(comment.Text, "#") || strings.ContainsAny(comment.Text, "\r\n") || comment.Start.Line < 1 || comment.Start.Column < 1 {
			return nil, fmt.Errorf("invalid physical source comment at %d:%d", comment.Start.Line, comment.Start.Column)
		}
		var placement annotationsbind.Placement
		switch comment.Placement {
		case facts.LeadingComment:
			placement = annotationsbind.PlacementLeading
		case facts.TrailingComment:
			placement = annotationsbind.PlacementTrailing
		default:
			return nil, fmt.Errorf("invalid source comment placement %d", comment.Placement)
		}
		result = append(result, annotationsbind.Comment{
			Text: comment.Text, Placement: placement,
			Start: invocationcontext.Position{Line: uint32(lowerU32(comment.Start.Line)), Column: uint32(lowerU32(comment.Start.Column))},
		})
	}
	return result, nil
}
