package langserver

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	"testing"
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
