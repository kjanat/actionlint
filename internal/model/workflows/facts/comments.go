package facts

import (
	"iter"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// SourceComments publishes YAML attachments independently of the decoded scalar.
func SourceComments(source iter.Seq[workflowlang.ScalarComment]) []SourceComment {
	if source == nil {
		return nil
	}
	var comments []SourceComment
	for comment := range source {
		placement := LeadingComment
		if comment.Trailing {
			placement = TrailingComment
		}
		comments = append(comments, SourceComment{Text: comment.Text, Start: Position(&comment.Pos), Placement: placement})
	}
	return comments
}

// CommentPlacement identifies a YAML comment's attachment to a declaration.
type CommentPlacement uint8

const (
	LeadingComment CommentPlacement = iota
	TrailingComment
)

// SourceComment is one physical YAML comment, including '#'. Start points to '#'.
type SourceComment struct {
	Text      string
	Start     results.Position
	Placement CommentPlacement
}
