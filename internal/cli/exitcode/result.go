// Package exitcode selects native command status and prepares command result documents.
package exitcode

import (
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

const (
	// ExitSuccessNoProblem means the command completed without findings.
	ExitSuccessNoProblem = 0
	// ExitSuccessProblemFound means the command completed with findings.
	ExitSuccessProblemFound = 1
	// ExitInvalidCommandOption means argument parsing or validation failed.
	ExitInvalidCommandOption = 2
	// ExitFailure means the requested operation could not complete.
	ExitFailure = 3
)

// AnalysisResult reports a completed check. A failed check must use NewCheckResult
// with its failure code and attach any partial analysis data separately.
func AnalysisResult(analysis report.Analysis) report.CheckResult {
	code := ExitSuccessNoProblem
	if len(analysis.Diagnostics) > 0 {
		code = ExitSuccessProblemFound
	}
	r := report.FromAnalysis(analysis)
	completion := NewCheckResult(code)
	r.Status, r.Completed, r.ExitCode = completion.Status, completion.Completed, completion.ExitCode
	return r
}

// NewCheckResult initializes a result with a consistent status and non-null lists.
func NewCheckResult(code int) report.CheckResult {
	r := report.CheckResult{SchemaVersion: 1, ExitCode: code, Diagnostics: []results.Diagnostic{}, Configs: []report.ResultConfig{}, Hints: []string{}, ActionMetadata: []report.ActionMetadataResult{}}
	switch code {
	case ExitSuccessNoProblem:
		r.Status, r.Completed = "success", true
	case ExitSuccessProblemFound:
		r.Status, r.Completed = "problems-found", true
	case ExitInvalidCommandOption:
		r.Status = "invalid-options"
	default:
		r.Status, r.ExitCode = "failure", ExitFailure
	}
	return r
}
