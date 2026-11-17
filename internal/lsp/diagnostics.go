package lsp

import (
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func diagnosticItems(doc Document, findings []results.Diagnostic, documents map[string]Document) []any {
	items := make([]any, 0, len(findings))
	for _, d := range findings {
		rangeValue, err := diagnosticRange([]byte(doc.Text), d.Start, d.End)
		if err != nil {
			continue
		}
		// LSP DiagnosticSeverity: 1 error, 2 warning, 3 information.
		level := 1
		switch d.Severity.Effective() {
		case severity.Warning:
			level = 2
		case severity.Info:
			level = 3
		case severity.Error:
		}
		msg := d.Message
		if d.Help != "" {
			msg += "\n\n" + d.Help
		}
		code := d.Rule
		if d.Code != "" {
			code += "/" + d.Code
		}
		item := map[string]any{"range": rangeValue, "severity": level, "code": code, "source": "actionlint", "message": msg}
		if d.DocumentationURL != "" {
			item["codeDescription"] = map[string]string{"href": d.DocumentationURL}
		}
		var related []any
		for _, location := range d.Related {
			target, ok := documents[location.Path]
			if !ok {
				continue
			}
			rangeValue, err := diagnosticRange([]byte(target.Text), location.Start, location.End)
			if err != nil {
				continue
			}
			related = append(related, map[string]any{"message": location.Message, "location": map[string]any{"uri": target.URI, "range": rangeValue}})
		}
		if len(related) > 0 {
			item["relatedInformation"] = related
		}
		items = append(items, item)
	}
	return items
}
