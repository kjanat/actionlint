package actionlint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OutputFormat selects a built-in diagnostic representation. The empty value
// retains the existing text, Oneline and Format behavior in LinterOptions.
type OutputFormat string

const (
	OutputFormatText    OutputFormat = "text"
	OutputFormatOneline OutputFormat = "oneline"
	OutputFormatJSON    OutputFormat = "json"
	OutputFormatJSONL   OutputFormat = "jsonl"
	OutputFormatSARIF   OutputFormat = "sarif"
	OutputFormatGitHub  OutputFormat = "github"
)

// DiagnosticPosition uses one-based Unicode character positions. Range ends are exclusive and may lie on a later line.
type DiagnosticPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Diagnostic is the structured CLI representation, separate from legacy template fields.
type Diagnostic struct {
	Rule    string             `json:"rule"`
	Message string             `json:"message"`
	Path    string             `json:"path"`
	Start   DiagnosticPosition `json:"start"`
	End     DiagnosticPosition `json:"end"`
	Snippet string             `json:"snippet,omitempty"`
}

// CheckResult is the versioned JSON document returned by a completed check.
type CheckResult struct {
	SchemaVersion int          `json:"schema_version"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// CheckSummary counts the selected inputs and reported findings.
type CheckSummary struct {
	Files    int `json:"files"`
	Findings int `json:"findings"`
}

func (e *Error) diagnostic(source []byte) Diagnostic {
	source = e.sourceFor(source)
	end := DiagnosticPosition{e.Line, e.Column + 1}
	snippet, _ := e.getLine(source)
	if snippet != "" {
		end.Column = e.getEndColumn(snippet) + 1
	}
	if e.endPosition != nil {
		end = DiagnosticPosition{e.endPosition.Line, e.endPosition.Col}
	}
	return Diagnostic{Rule: e.Kind, Message: e.Message, Path: e.Filepath,
		Start: DiagnosticPosition{e.Line, e.Column}, End: end, Snippet: snippet}
}

// legacyError adapts a canonical half-open span to the legacy renderer's inclusive columns.
func (d Diagnostic) legacyError() *Error {
	e := &Error{Kind: d.Rule, Message: d.Message, Filepath: d.Path, Line: d.Start.Line, Column: d.Start.Column,
		endPosition: &Pos{Line: d.End.Line, Col: d.End.Column}}
	if d.End.Line == d.Start.Line && d.End.Column > d.Start.Column {
		e.endColumn = d.End.Column - 1
	}
	return e
}

func writeDiagnostics(out io.Writer, diagnostics []Diagnostic, lines bool) error {
	enc := json.NewEncoder(out)
	if lines {
		for _, d := range diagnostics {
			if err := enc.Encode(struct {
				SchemaVersion int `json:"schema_version"`
				Diagnostic
			}{1, d}); err != nil {
				return err
			}
		}
		return nil
	}
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	return enc.Encode(CheckResult{SchemaVersion: 1, Diagnostics: diagnostics})
}

type diagnosticFormatter interface {
	Print(io.Writer, []*ErrorTemplateFields) error
}

type githubDiagnosticFormatter struct{}

func (githubDiagnosticFormatter) Print(out io.Writer, fields []*ErrorTemplateFields) error {
	data := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	property := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	for _, f := range fields {
		if _, err := fmt.Fprintf(out, "::error file=%s,line=%d,col=%d,endColumn=%d,title=%s::%s\n", property.Replace(f.Filepath), f.Line, f.Column, f.EndColumn, property.Replace(f.Kind), data.Replace(f.Message)); err != nil {
			return err
		}
	}
	return nil
}
