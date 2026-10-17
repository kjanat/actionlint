package engine

import "actionlint.kjanat.dev/internal/render"

type OutputFormat = render.Format

const (
	OutputFormatText    = render.FormatText
	OutputFormatOneline = render.FormatOneline
	OutputFormatJSON    = render.FormatJSON
	OutputFormatJSONL   = render.FormatJSONL
	OutputFormatSARIF   = render.FormatSARIF
	OutputFormatGitHub  = render.FormatGitHub
)

type CheckResult = render.CheckResult
type ResultConfig = render.ResultConfig
type CheckSummary = render.CheckSummary

func NewCheckResult(code int) CheckResult { return render.NewCheckResult(code) }

const (
	// ExitStatusSuccessNoProblem means linting completed without findings.
	ExitStatusSuccessNoProblem = render.ExitSuccessNoProblem
	// ExitStatusSuccessProblemFound means linting completed with findings.
	ExitStatusSuccessProblemFound = render.ExitSuccessProblemFound
	// ExitStatusInvalidCommandOption means command-line parsing or validation failed.
	ExitStatusInvalidCommandOption = render.ExitInvalidCommandOption
	// ExitStatusFailure means linting could not complete.
	ExitStatusFailure = render.ExitFailure
)
