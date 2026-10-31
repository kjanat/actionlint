package langserver

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestDiagnosticCodesAreQualifiedByRule(t *testing.T) {
	doc := Document{Text: "value"}
	findings := []diagnostic.Diagnostic{
		{Rule: "expression-reference", Code: "unknown-property", Message: "reference", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 6}},
		{Rule: "event-payload", Code: "unknown-property", Message: "payload", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 6}},
	}
	items := diagnosticItems(doc, findings, nil)
	for i, finding := range findings {
		if got := items[i].(map[string]any)["code"]; got != finding.Rule+"/"+finding.Code {
			t.Fatalf("diagnostic identity lost: %v", got)
		}
	}
}

func TestDiagnosticSeverities(t *testing.T) {
	for _, tc := range []struct {
		severity string
		want     int
	}{
		{"error", 1}, {"warning", 2}, {"warn", 2}, {"info", 3},
	} {
		t.Run(tc.severity, func(t *testing.T) {
			doc := Document{Text: "value"}
			finding := diagnostic.Diagnostic{
				Rule: "shellcheck", Severity: tc.severity, Message: "finding",
				Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 6},
			}
			items := diagnosticItems(doc, []diagnostic.Diagnostic{finding}, nil)
			item, ok := items[0].(map[string]any)
			if !ok || item["severity"] != tc.want {
				t.Fatalf("diagnostic=%v; want severity %d", items[0], tc.want)
			}
		})
	}
}
