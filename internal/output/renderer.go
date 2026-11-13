package output

import (
	"errors"
	"fmt"
	"io"
)

// Renderer formats analysis results.
type Renderer struct {
	format     Format
	oneline    bool
	version    func() string
	rules      *ruleSet
	formatter  *Formatter
	width      int
	hyperlinks bool
	color      bool
}

// New validates a built-in format or custom Go template before rendering.
func New(format Format, template string, oneline bool, version func() string, rules []Rule) (*Renderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &Renderer{format: format, oneline: oneline, version: version, rules: newRuleSet(rules), width: 100}
	switch format {
	case "", FormatPretty, FormatGCC, FormatJSON, FormatJSONL, FormatSARIF, FormatGitHub:
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

// SetHyperlinks enables OSC 8 links in pretty diagnostics.
func (r *Renderer) SetHyperlinks(enabled bool) { r.hyperlinks = enabled }

func (r *Renderer) SetColor(enabled bool) { r.color = enabled }

// SetWidth sets the pretty format's display width, including indentation.
func (r *Renderer) SetWidth(width int) error {
	if width < 40 || width > 400 {
		return fmt.Errorf("diagnostic width must be between 40 and 400, got %d", width)
	}
	r.width = width
	return nil
}

// Render writes the result in the selected format.
func (r *Renderer) Render(out io.Writer, result Result) error {
	for _, rule := range result.Rules {
		r.rules.register(rule)
	}
	switch r.format {
	case FormatGCC:
		return WriteGCC(out, result.Diagnostics)
	case FormatJSON, FormatJSONL:
		if result.Status == "" {
			return errors.New("JSON rendering requires a command result with an explicit status")
		}
		return WriteJSON(out, result.CheckResult, r.format == FormatJSONL)
	case FormatGitHub:
		return WriteGitHub(out, result.Diagnostics)
	case FormatSARIF:
		return WriteSARIF(out, r.version(), r.rules.sorted(), result.Diagnostics)
	case FormatText, FormatPretty, FormatOneline:
		// Text formats render findings below.
	}
	fields := make([]*TemplateFields, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if r.format == FormatPretty {
			configPath := ""
			// Omit repeated rule descriptions from individual findings.
			if rule := r.rules.lookup(finding.Diagnostic.Rule); rule != nil {
				if rule.Category != "" {
					configPath = "lint.rules." + rule.Category + "." + rule.Name
				}
				if finding.Diagnostic.Help == rule.Help {
					finding.Diagnostic.Help = ""
				}
				if finding.Diagnostic.Explanation == rule.Explanation {
					finding.Diagnostic.Explanation = ""
				}
				if finding.Diagnostic.DocumentationURL == rule.DocumentationURL {
					finding.Diagnostic.DocumentationURL = ""
				}
			}
			if err := finding.printPretty(out, r.width, r.hyperlinks, r.color, configPath); err != nil {
				return err
			}
			continue
		}
		if r.formatter != nil {
			fields = append(fields, finding.TemplateFields())
			continue
		}
		if r.oneline {
			finding.Source = nil
			finding.canonicalSnippet = ""
		}
		finding.prettyPrint(out, r.color)
	}
	if r.formatter != nil {
		return r.formatter.Print(out, fields)
	}
	return nil
}
