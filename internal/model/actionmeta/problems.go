package actionmeta

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// MetadataProblem describes invalid manifest structure or an expression issue.
// It has no rule, severity, suppression state or proposed source edits.
// Inspection sets Path to the manifest it read. Remote resolvers replace it with
// the manifest's repository path before reporting the problem.
type MetadataProblem struct {
	Code    ProblemCode         `json:"code,omitempty"`
	Message string              `json:"message"`
	Path    string              `json:"path"`
	Start   sourcetext.Position `json:"start"`
	End     sourcetext.Position `json:"end"`
}
