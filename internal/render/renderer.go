package render

import (
	"errors"
	"fmt"
	"io"
)

// Renderer formats results and retains rule metadata across calls.
type Renderer struct {
	format    Format
	oneline   bool
	version   func() string
	rules     *ruleSet
	formatter *Formatter
}

// New validates a built-in format or custom Go template before rendering.
func New(format Format, template string, oneline bool, version func() string, rules []Rule) (*Renderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &Renderer{format: format, oneline: oneline, version: version, rules: newRuleSet(rules)}
	switch format {
	case "", FormatJSON, FormatJSONL, FormatSARIF, FormatGitHub:
	case FormatText:
		r.oneline = false
	case FormatOneline:
		r.oneline = true
	default:
		return nil, fmt.Errorf("unknown output format %q", format)
	}
	if template != "" {
		formatter, err := newFormatter(template, version, r.rules)
		if err != nil {
			return nil, err
		}
		r.formatter = formatter
	}
	return r, nil
}

// Formatter returns the template formatter, or nil for built-in formats.
func (r *Renderer) Formatter() *Formatter { return r.formatter }

// Render writes the result in the selected format.
func (r *Renderer) Render(out io.Writer, result Result) error {
	for _, rule := range result.Rules {
		r.rules.register(rule)
	}
	switch r.format {
	case FormatJSON, FormatJSONL:
		report := result.CheckResult()
		return report.WriteJSON(out, r.format == FormatJSONL)
	case FormatGitHub:
		return WriteGitHub(out, result.Diagnostics)
	case FormatSARIF:
		return WriteSARIF(out, r.version(), r.rules.sorted(), result.Diagnostics)
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
