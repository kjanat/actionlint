package actionmeta

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
)

// MetadataProblem describes invalid manifest structure or an expression issue.
// It has no rule, severity, suppression state or proposed source edits.
// Inspection sets Path to the manifest it read. Remote resolvers replace it with
// the manifest's repository path before reporting the problem.
type MetadataProblem struct {
	Code    ProblemCode     `json:"code,omitempty"`
	Message string          `json:"message"`
	Path    string          `json:"path"`
	Start   source.Position `json:"start"`
	End     source.Position `json:"end"`
}
