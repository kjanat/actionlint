package engine

import (
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/render"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// OutputFormat selects a built-in diagnostic representation. The empty value
// retains the existing text, Oneline and Format behavior in LinterOptions.
type OutputFormat = render.Format

const (
	OutputFormatText    = render.FormatText
	OutputFormatOneline = render.FormatOneline
	OutputFormatJSON    = render.FormatJSON
	OutputFormatJSONL   = render.FormatJSONL
	OutputFormatSARIF   = render.FormatSARIF
	OutputFormatGitHub  = render.FormatGitHub
)

type (
	DiagnosticPosition = diagnostic.Position
	Diagnostic         = diagnostic.Diagnostic
	DiagnosticFix      = diagnostic.Fix
	DiagnosticEdit     = diagnostic.Edit
	CheckResult        = render.CheckResult
	ResultConfig       = render.ResultConfig
	CheckSummary       = render.CheckSummary
)

// NewCheckResult initializes a result with a consistent status and non-null lists.
func NewCheckResult(code int) CheckResult { return render.NewCheckResult(code) }

func (e *Error) diagnostic(source []byte) Diagnostic {
	source = e.sourceFor(source)
	end := DiagnosticPosition{Line: e.Line, Column: e.Column + 1}
	snippet, _ := render.SourceLine(source, e.Line, e.rawScriptSource)
	if snippet != "" {
		end.Column = render.InclusiveEnd(snippet, e.Column, e.endColumn) + 1
	}
	if e.endPosition != nil {
		end = DiagnosticPosition{Line: e.endPosition.Line, Column: e.endPosition.Col}
	}
	var fixes []DiagnosticFix
	if len(e.fixes) > 0 {
		fixes = make([]DiagnosticFix, len(e.fixes))
	}
	for i, fix := range e.fixes {
		fixes[i] = DiagnosticFix{Description: fix.Description, Edits: make([]DiagnosticEdit, len(fix.Edits))}
		for j, edit := range fix.Edits {
			if edit.Path == "" {
				edit.Path = e.Filepath
			}
			fixes[i].Edits[j] = edit
		}
	}
	return Diagnostic{
		Rule: e.Kind, Code: e.code, Severity: e.severity, Message: e.Message, Path: e.Filepath,
		Start: DiagnosticPosition{Line: e.Line, Column: e.Column}, End: end, Snippet: snippet, Fixes: fixes,
	}
}

// legacyDiagnosticError adapts a canonical half-open span to the legacy renderer's inclusive columns.
func legacyDiagnosticError(d Diagnostic) *Error {
	e := &Error{
		Kind: d.Rule, Message: d.Message, Filepath: d.Path, Line: d.Start.Line, Column: d.Start.Column,
		endPosition: &workflowlang.Pos{Line: d.End.Line, Col: d.End.Column}, code: d.Code, severity: d.Severity, fixes: d.Fixes,
	}
	if d.End.Line == d.Start.Line && d.End.Column > d.Start.Column {
		e.endColumn = d.End.Column - 1
	}
	return e
}
