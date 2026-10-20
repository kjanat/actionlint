// Package render presents analysis results as text, Go templates, JSON, SARIF and GitHub annotations.
package render

// Format selects a built-in diagnostic representation. The empty value retains
// the legacy text, oneline and custom-template behaviour.
type Format string

const (
	FormatText    Format = "text"
	FormatOneline Format = "oneline"
	FormatJSON    Format = "json"
	FormatJSONL   Format = "jsonl"
	FormatSARIF   Format = "sarif"
	FormatGitHub  Format = "github"
)

const (
	// ExitSuccessNoProblem means linting completed without findings.
	ExitSuccessNoProblem = 0
	// ExitSuccessProblemFound means linting completed with findings.
	ExitSuccessProblemFound = 1
	// ExitInvalidCommandOption means command-line parsing or validation failed.
	ExitInvalidCommandOption = 2
	// ExitFailure means linting could not complete.
	ExitFailure = 3
)
