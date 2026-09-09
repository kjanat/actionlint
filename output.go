package actionlint

import (
	"encoding/json"
	"fmt"
	"io"
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
)

type diagnosticFormatter interface {
	Print(io.Writer, []*ErrorTemplateFields) error
	PrintErrors(io.Writer, []*Error, []byte) error
}

type jsonDiagnosticFormatter struct{ lines bool }

func (f jsonDiagnosticFormatter) Print(out io.Writer, fields []*ErrorTemplateFields) error {
	enc := json.NewEncoder(out)
	if f.lines {
		for _, field := range fields {
			if err := enc.Encode(field); err != nil {
				return fmt.Errorf("could not write JSON diagnostic: %w", err)
			}
		}
		return nil
	}
	if fields == nil {
		fields = []*ErrorTemplateFields{}
	}
	if err := enc.Encode(fields); err != nil {
		return fmt.Errorf("could not write JSON diagnostics: %w", err)
	}
	return nil
}

func (f jsonDiagnosticFormatter) PrintErrors(out io.Writer, errs []*Error, src []byte) error {
	fields := make([]*ErrorTemplateFields, 0, len(errs))
	for _, err := range errs {
		fields = append(fields, err.GetTemplateFields(src))
	}
	return f.Print(out, fields)
}
