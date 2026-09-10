package actionlint

import (
	"errors"
	"fmt"
	"io"
)

// AnalysisRenderer formats analysis results and retains custom-template rule metadata across calls.
type AnalysisRenderer struct {
	format    OutputFormat
	oneline   bool
	formatter diagnosticFormatter
}

// NewAnalysisRenderer validates a built-in format or custom Go template before rendering.
func NewAnalysisRenderer(format OutputFormat, template string, oneline bool) (*AnalysisRenderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &AnalysisRenderer{format: format, oneline: oneline}
	switch format {
	case "", OutputFormatText, OutputFormatJSON, OutputFormatJSONL:
	case OutputFormatOneline:
		r.oneline = true
	case OutputFormatSARIF:
		template = SARIFTemplate()
	case OutputFormatGitHub:
		r.formatter = githubDiagnosticFormatter{}
	default:
		return nil, fmt.Errorf("unknown output format %q", format)
	}
	if template != "" {
		formatter, err := NewErrorFormatter(template)
		if err != nil {
			return nil, err
		}
		r.formatter = formatter
	}
	return r, nil
}

// Render writes findings using their original sources and the selected output format.
func (r *AnalysisRenderer) Render(out io.Writer, result *AnalysisResult) error {
	if r.format == OutputFormatJSON || r.format == OutputFormatJSONL {
		return writeDiagnostics(out, result.Diagnostics, r.format == OutputFormatJSONL)
	}
	if formatter, ok := r.formatter.(*ErrorFormatter); ok {
		for _, file := range result.files {
			for _, rule := range file.rules {
				formatter.RegisterRule(rule)
			}
		}
	}
	fields := make([]*ErrorTemplateFields, 0, len(result.Diagnostics))
	index := 0
	for _, file := range result.files {
		for _, original := range file.errors {
			finding := result.Diagnostics[index].legacyError()
			index++
			source := original.sourceFor(file.source.Content)
			if r.formatter != nil {
				fields = append(fields, finding.GetTemplateFields(source))
			} else {
				if r.oneline {
					source = nil
				}
				finding.PrettyPrint(out, source)
			}
		}
	}
	if r.formatter != nil {
		return r.formatter.Print(out, fields)
	}
	return nil
}
