package action

import "actionlint.kjanat.dev/v2/internal/results"

// MetadataProblem describes invalid manifest structure or an expression issue.
// It has no rule, severity, suppression state or proposed source edits.
type MetadataProblem struct {
	Code    ProblemCode
	Path    string
	Message string
	Start   results.Position
	End     results.Position
}
