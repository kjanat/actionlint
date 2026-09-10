package actionlint

import (
	"errors"
	"fmt"
	"io"
)

type analysisRenderer struct {
	format    OutputFormat
	oneline   bool
	formatter diagnosticFormatter
}

func newAnalysisRenderer(format OutputFormat, template string, oneline bool) (*analysisRenderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &analysisRenderer{format: format, oneline: oneline}
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

func (r *analysisRenderer) render(out io.Writer, result *AnalysisResult) error {
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
