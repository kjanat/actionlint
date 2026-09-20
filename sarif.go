package actionlint

import (
	_ "embed"
	"fmt"
	"io"
)

//go:embed sarif_template.txt
var sarifTemplate string

// SARIFTemplate returns the canonical Go template for SARIF output.
func SARIFTemplate() string {
	return sarifTemplate
}

// Native SARIF uses exclusive, possibly multiline ranges. Legacy custom templates
// retain their inclusive, single-line ErrorTemplateFields representation.
type sarifTemplateFields struct {
	ErrorTemplateFields
	EndLine int
}

func (f *ErrorFormatter) printSARIF(out io.Writer, diagnostics []Diagnostic) error {
	fields := make([]sarifTemplateFields, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		fields = append(fields, sarifTemplateFields{
			ErrorTemplateFields: ErrorTemplateFields{
				Message: diagnostic.Message, Filepath: diagnostic.Path, Kind: diagnostic.Rule,
				Line: diagnostic.Start.Line, Column: diagnostic.Start.Column,
				EndColumn: diagnostic.End.Column, Snippet: diagnostic.Snippet,
			},
			EndLine: diagnostic.End.Line,
		})
	}
	if err := f.temp.Execute(out, fields); err != nil {
		return fmt.Errorf("could not format error messages: %w", err)
	}
	return nil
}
