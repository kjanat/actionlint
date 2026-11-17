package lsp

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestDiagnosticCodesAreQualifiedByRule(t *testing.T) {
	t.Parallel()
	doc := Document{Text: "value"}
	findings := []results.Diagnostic{
		{Rule: "expression-reference", Code: "unknown-property", Message: "reference", Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 6}},
		{Rule: "event-payload", Code: "unknown-property", Message: "payload", Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 6}},
	}
	items := diagnosticItems(doc, findings, nil)
	for i, finding := range findings {
		if got := items[i].(map[string]any)["code"]; got != finding.Rule+"/"+finding.Code {
			t.Fatalf("diagnostic identity lost: %v", got)
		}
	}
}

func TestDiagnosticSeverities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		severity string
		want     int
	}{
		{"", 1}, {"error", 1}, {"warning", 2}, {"info", 3},
	} {
		t.Run(tc.severity, func(t *testing.T) {
			t.Parallel()
			doc := Document{Text: "value"}
			finding := results.Diagnostic{
				Rule: "shellcheck", Severity: severity.Severity(tc.severity), Message: "finding",
				Start: sourcetext.Position{Line: 1, Column: 1}, End: sourcetext.Position{Line: 1, Column: 6},
			}
			items := diagnosticItems(doc, []results.Diagnostic{finding}, nil)
			item, ok := items[0].(map[string]any)
			if !ok || item["severity"] != tc.want {
				t.Fatalf("diagnostic=%v; want severity %d", items[0], tc.want)
			}
		})
	}
}
