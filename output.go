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

// DiagnosticPosition uses one-based Unicode character positions. Range ends are inclusive.
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

func checkResult(fields []*ErrorTemplateFields) CheckResult {
	result := CheckResult{SchemaVersion: 1, Diagnostics: make([]Diagnostic, 0, len(fields))}
	for _, f := range fields {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{f.Kind, f.Message, f.Filepath, DiagnosticPosition{f.Line, f.Column}, DiagnosticPosition{f.Line, f.EndColumn}, f.Snippet})
	}
	return result
}

type diagnosticFormatter interface {
	Print(io.Writer, []*ErrorTemplateFields) error
	PrintErrors(io.Writer, []*Error, []byte) error
}

type jsonDiagnosticFormatter struct{ lines bool }

func (f jsonDiagnosticFormatter) Print(out io.Writer, fields []*ErrorTemplateFields) error {
	enc := json.NewEncoder(out)
	result := checkResult(fields)
	if f.lines {
		for _, field := range result.Diagnostics {
			if err := enc.Encode(field); err != nil {
				return fmt.Errorf("could not write JSON diagnostic: %w", err)
			}
		}
		return nil
	}
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("could not write JSON diagnostics: %w", err)
	}
	return nil
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

func (f githubDiagnosticFormatter) PrintErrors(out io.Writer, errs []*Error, src []byte) error {
	fields := make([]*ErrorTemplateFields, 0, len(errs))
	for _, err := range errs {
		fields = append(fields, err.GetTemplateFields(src))
	}
	return f.Print(out, fields)
}

func (f jsonDiagnosticFormatter) PrintErrors(out io.Writer, errs []*Error, src []byte) error {
	fields := make([]*ErrorTemplateFields, 0, len(errs))
	for _, err := range errs {
		fields = append(fields, err.GetTemplateFields(src))
	}
	return f.Print(out, fields)
}
