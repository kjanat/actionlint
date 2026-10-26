package component

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/source"
)

type sourceCommentsKey struct{}

func suppliedComments(input analyzer.Input) map[source.Reference][]analyzer.SourceComment {
	comments := make(map[source.Reference][]analyzer.SourceComment)
	add := func(script analyzer.Script) {
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

func (sourceAnnotations) Comments(ctx context.Context, reference annotationsbind.SourceReference) ([]annotationsbind.Comment, error) {
	comments, _ := ctx.Value(sourceCommentsKey{}).(map[source.Reference][]analyzer.SourceComment)
	key := source.Reference{Document: reference.Document, Revision: reference.Revision, Node: reference.Node, Address: reference.Address}
	var result []annotationsbind.Comment
	for _, comment := range comments[key] {
		if !strings.HasPrefix(comment.Text, "#") || strings.ContainsAny(comment.Text, "\r\n") || comment.Start.Line < 1 || comment.Start.Column < 1 {
			return nil, fmt.Errorf("invalid physical source comment at %d:%d", comment.Start.Line, comment.Start.Column)
		}
		var placement annotationsbind.Placement
		switch comment.Placement {
		case analyzer.LeadingComment:
			placement = annotationsbind.PlacementLeading
		case analyzer.TrailingComment:
			placement = annotationsbind.PlacementTrailing
		default:
			return nil, fmt.Errorf("invalid source comment placement %d", comment.Placement)
		}
		result = append(result, annotationsbind.Comment{
			Text: comment.Text, Placement: placement,
			Start: annotationsbind.Position{Line: uint32(lowerU32(comment.Start.Line)), Column: uint32(lowerU32(comment.Start.Column))},
		})
	}
	return result, nil
}
