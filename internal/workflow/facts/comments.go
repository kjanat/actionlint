package facts

import (
	"iter"

	"actionlint.kjanat.dev/v2/analyzer"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// SourceComments publishes YAML attachments independently of the decoded scalar.
func SourceComments(source iter.Seq[workflowlang.ScalarComment]) []analyzer.SourceComment {
	if source == nil {
		return nil
	}
	var comments []analyzer.SourceComment
	for comment := range source {
		placement := analyzer.LeadingComment
		if comment.Trailing {
			placement = analyzer.TrailingComment
		}
		comments = append(comments, analyzer.SourceComment{Text: comment.Text, Start: Position(&comment.Pos), Placement: placement})
	}
	return comments
}
