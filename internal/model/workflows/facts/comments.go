package facts

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// SourceComments publishes YAML attachments independently of the decoded scalar.
func SourceComments(source []definition.ScalarComment) []SourceComment {
	comments := make([]SourceComment, 0, len(source))
	for _, comment := range source {
		placement := LeadingComment
		if comment.Trailing {
			placement = TrailingComment
		}
		comments = append(comments, SourceComment{Text: comment.Text, Start: comment.Pos, Placement: placement})
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
	Start     sourcetext.Position
	Placement CommentPlacement
}
