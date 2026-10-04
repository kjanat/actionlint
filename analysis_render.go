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
	formatter *ErrorFormatter
}

// NewAnalysisRenderer validates a built-in format or custom Go template before rendering.
func NewAnalysisRenderer(format OutputFormat, template string, oneline bool) (*AnalysisRenderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &AnalysisRenderer{format: format, oneline: oneline}
	switch format {
	case "", OutputFormatJSON, OutputFormatJSONL:
	case OutputFormatText:
		r.oneline = false
	case OutputFormatOneline:
		r.oneline = true
	case OutputFormatSARIF:
		template = SARIFTemplate()
	case OutputFormatGitHub:
	default:
		return nil, fmt.Errorf("unknown output format %q", format)
	}
	if template != "" {
		formatter, err := NewErrorFormatter(template)
		if err != nil {
			return nil, err
		}
		r.formatter = formatter
		if format == OutputFormatSARIF {
			if _, err := formatter.temp.Parse(`{{define "sarifEndLine"}}"endLine": {{.EndLine}},{{end}}
{{define "sarifColumnKind"}}"columnKind": "unicodeCodePoints",{{end}}
{{define "sarifURIBaseID"}}{{if .URIBaseID}},"uriBaseId": {{json .URIBaseID}}{{end}}{{end}}
{{define "sarifMetadata"}}"level": {{json .Level}},
{{if .Code}}"properties": {"externalCode": {{json .Code}}},{{end}}
{{if .Fixes}}"fixes": {{json .Fixes}},{{end}}{{end}}`); err != nil {
				return nil, fmt.Errorf("could not parse SARIF range template: %w", err)
			}
		}
	}
	return r, nil
}

// Render writes findings using their original sources and the selected output format.
func (r *AnalysisRenderer) Render(out io.Writer, result *AnalysisResult) error {
	if r.format == OutputFormatJSON || r.format == OutputFormatJSONL {
		report := result.CheckResult()
		return report.WriteJSON(out, r.format == OutputFormatJSONL)
	}
	if r.format == OutputFormatGitHub {
		return writeGitHubDiagnostics(out, result.Diagnostics)
	}
	if r.formatter != nil {
		for _, file := range result.files {
			for _, rule := range file.rules {
				r.formatter.RegisterRule(rule)
			}
		}
	}
	if r.format == OutputFormatSARIF {
		return r.formatter.printSARIF(out, result.Diagnostics)
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

// CheckResult exports a completed analysis using the shared public result contract.
func (r *AnalysisResult) CheckResult() CheckResult {
	code := ExitStatusSuccessNoProblem
	if len(r.Diagnostics) > 0 {
		code = ExitStatusSuccessProblemFound
	}
	result := NewCheckResult(code)
	count := r.FileCount()
	result.FileCount = &count
	if r.Diagnostics != nil {
		result.Diagnostics = r.Diagnostics
	}
	for _, config := range r.Configurations {
		result.AddConfiguration(config)
	}
	return result
}
