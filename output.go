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
	Rule string `json:"rule"`
	// Code and Severity retain an external analyzer's identifier and native level.
	// Empty values mean the rule did not supply this metadata.
	Code     string             `json:"code,omitempty"`
	Severity string             `json:"severity,omitempty"`
	Message  string             `json:"message"`
	Path     string             `json:"path"`
	Start    DiagnosticPosition `json:"start"`
	End      DiagnosticPosition `json:"end"`
	Snippet  string             `json:"snippet,omitempty"`
	Fixes    []DiagnosticFix    `json:"fixes,omitempty"`
}

// DiagnosticFix is one complete suggested change. Apply all edits together.
type DiagnosticFix struct {
	Description string           `json:"description"`
	Edits       []DiagnosticEdit `json:"edits"`
}

// DiagnosticEdit replaces a half-open Unicode range in the original source file.
type DiagnosticEdit struct {
	Path        string             `json:"path"`
	Start       DiagnosticPosition `json:"start"`
	End         DiagnosticPosition `json:"end"`
	Replacement string             `json:"replacement"`
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
	return Diagnostic{Rule: e.Kind, Code: e.code, Severity: e.severity, Message: e.Message, Path: e.Filepath,
		Start: DiagnosticPosition{e.Line, e.Column}, End: end, Snippet: snippet, Fixes: fixes}
}

// legacyError adapts a canonical half-open span to the legacy renderer's inclusive columns.
func (d Diagnostic) legacyError() *Error {
	e := &Error{Kind: d.Rule, Message: d.Message, Filepath: d.Path, Line: d.Start.Line, Column: d.Start.Column,
		endPosition: &Pos{Line: d.End.Line, Col: d.End.Column}, code: d.Code, severity: d.Severity, fixes: d.Fixes}
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

func writeGitHubDiagnostics(out io.Writer, diagnostics []Diagnostic) error {
	data := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	property := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	for _, diagnostic := range diagnostics {
		endLine := diagnostic.End.Line
		if endLine > diagnostic.Start.Line && diagnostic.End.Column == 1 {
			endLine-- // An exclusive end at the next line's start does not include that line.
		}
		position := fmt.Sprintf("line=%d,endLine=%d", diagnostic.Start.Line, endLine)
		// GitHub rejects column properties on multiline annotations.
		if diagnostic.Start.Line == diagnostic.End.Line {
			position += fmt.Sprintf(",col=%d,endColumn=%d", diagnostic.Start.Column, max(diagnostic.Start.Column, diagnostic.End.Column-1))
		}
		if _, err := fmt.Fprintf(out, "::error file=%s,%s,title=%s::%s\n", property.Replace(diagnostic.Path), position, property.Replace(diagnostic.Rule), data.Replace(diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}
