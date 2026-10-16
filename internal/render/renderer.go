package render

import (
	"errors"
	"fmt"
	"io"
)

// Renderer formats results and retains custom-template rule metadata across calls.
type Renderer struct {
	format    Format
	oneline   bool
	formatter *Formatter
}

// New validates a built-in format or custom Go template before rendering.
func New(format Format, template string, oneline bool, version func() string, rules []Rule) (*Renderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &Renderer{format: format, oneline: oneline}
	switch format {
	case "", FormatJSON, FormatJSONL:
	case FormatText:
		r.oneline = false
	case FormatOneline:
		r.oneline = true
	case FormatSARIF:
		template = SARIFTemplate()
	case FormatGitHub:
	default:
		return nil, fmt.Errorf("unknown output format %q", format)
	}
	if template != "" {
		formatter, err := NewFormatter(template, version, rules)
		if err != nil {
			return nil, err
		}
		r.formatter = formatter
		if format == FormatSARIF {
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

// Formatter returns the template formatter, or nil for built-in formats without one.
func (r *Renderer) Formatter() *Formatter { return r.formatter }

// Render writes the result in the selected format.
func (r *Renderer) Render(out io.Writer, result Result) error {
	if r.format == FormatJSON || r.format == FormatJSONL {
		report := result.CheckResult()
		return report.WriteJSON(out, r.format == FormatJSONL)
	}
	if r.format == FormatGitHub {
		return WriteGitHub(out, result.Diagnostics)
	}
	if r.formatter != nil {
		for _, rule := range result.Rules {
			r.formatter.RegisterRule(rule)
		}
	}
	if r.format == FormatSARIF {
		return r.formatter.printSARIF(out, result.Diagnostics)
	}
	fields := make([]*TemplateFields, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if r.formatter != nil {
			fields = append(fields, finding.TemplateFields())
			continue
		}
		if r.oneline {
			finding.Source = nil
		}
		finding.PrettyPrint(out)
	}
	if r.formatter != nil {
		return r.formatter.Print(out, fields)
	}
	return nil
}
