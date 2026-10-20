package langserver

import "actionlint.kjanat.dev/diagnostic"

func diagnosticItems(doc Document, findings []diagnostic.Diagnostic, documents map[string]Document) []any {
	items := make([]any, 0, len(findings))
	for _, d := range findings {
		severity := 1
		switch d.Severity {
		case "warning", "warn":
			severity = 2
		case "info":
			severity = 3
		}
		msg := d.Message
		if d.Help != "" {
			msg += "\n\n" + d.Help
		}
		code := d.Rule
		if d.Code != "" {
			code += "/" + d.Code
		}
		item := map[string]any{"range": diagnosticRange([]byte(doc.Text), d.Start, d.End), "severity": severity, "code": code, "source": "actionlint", "message": msg}
		if d.DocumentationURL != "" {
			item["codeDescription"] = map[string]string{"href": d.DocumentationURL}
		}
		var related []any
		for _, location := range d.Related {
			target, ok := documents[location.Path]
			if !ok {
				continue
			}
			related = append(related, map[string]any{"message": location.Message, "location": map[string]any{"uri": target.URI, "range": diagnosticRange([]byte(target.Text), location.Start, location.End)}})
		}
		if len(related) > 0 {
			item["relatedInformation"] = related
		}
		items = append(items, item)
	}
	return items
}
