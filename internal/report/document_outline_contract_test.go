package report_test

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/internal/report"
)

func TestDocumentOutlinesRejectUnknownKinds(t *testing.T) {
	for _, input := range []string{
		`[null]`,
		`[{"kind":"unexpected"}]`,
		`[{"kind":"action","runs":{"kind":"unexpected"}}]`,
		`[{"kind":"action","runs":null}]`,
		`[{"kind":"action"}]`,
	} {
		var documents report.DocumentOutlines
		if err := json.Unmarshal([]byte(input), &documents); err == nil {
			t.Fatalf("accepted unsupported document/runtime: %s", input)
		}
	}
}
