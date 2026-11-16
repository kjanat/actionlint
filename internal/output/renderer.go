package output

import (
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Renderer formats analysis results.
type Renderer struct {
	format     outputformat.Format
	oneline    bool
	version    func() string
	rules      *ruleSet
	formatter  *Formatter
	width      int
	hyperlinks bool
	color      bool
}

// New validates a built-in format or custom Go template before rendering.
func New(format outputformat.Format, template string, oneline bool, version func() string, rules []Rule) (*Renderer, error) {
	if format != "" && template != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	r := &Renderer{format: format, oneline: oneline, version: version, rules: newRuleSet(rules), width: 100}
	switch format {
	case "", outputformat.Pretty, outputformat.GCC, outputformat.JSON, outputformat.JSONL, outputformat.SARIF, outputformat.GitHub:
	case outputformat.Text:
		r.oneline = false
	case outputformat.Oneline:
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
func (r *Renderer) Render(out io.Writer, result RenderedReport) error {
	for _, rule := range result.Rules {
		r.rules.register(rule)
	}
	switch r.format {
	case outputformat.GCC:
		return WriteGCC(out, result.Diagnostics)
	case outputformat.JSON, outputformat.JSONL:
		if result.Status == "" {
			return errors.New("JSON rendering requires a command result with an explicit status")
		}
		return WriteJSON(out, result.CheckResult, r.format == outputformat.JSONL)
	case outputformat.GitHub:
		return WriteGitHub(out, result.Diagnostics)
	case outputformat.SARIF:
		return WriteSARIF(out, r.version(), r.rules.sorted(), result.Diagnostics)
	case outputformat.Text, outputformat.Pretty, outputformat.Oneline:
		// Text formats render findings below.
	}
	fields := make([]*TemplateFields, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if r.format == outputformat.Pretty {
			configPath := ""
			// Omit repeated rule descriptions from individual findings.
			if rule := r.rules.lookup(finding.Rule); rule != nil {
				if rule.Category != "" {
					configPath = "lint.rules." + rule.Category + "." + rule.Name
				}
				if finding.Help == rule.Help {
					finding.Help = ""
				}
				if finding.Explanation == rule.Explanation {
					finding.Explanation = ""
				}
				if finding.DocumentationURL == rule.DocumentationURL {
					finding.DocumentationURL = ""
				}
			}
			if err := printPretty(out, finding, r.width, r.hyperlinks, r.color, configPath); err != nil {
				return err
			}
			continue
		}
		if r.formatter != nil {
			fields = append(fields, TemplateFieldsOf(finding))
			continue
		}
		if r.oneline {
			finding.Excerpt = results.Excerpt{}
		}
		prettyPrint(out, finding, r.color)
	}
	if r.formatter != nil {
		return r.formatter.Print(out, fields)
	}
	return nil
}
