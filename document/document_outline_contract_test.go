package document_test

import (
	"encoding/json"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"
)

func TestDocumentOutlinesRejectUnknownKinds(t *testing.T) {
	for _, input := range []string{
		`[null]`,
		`[{"kind":"unexpected"}]`,
		`[{"kind":"action","runs":{"kind":"unexpected"}}]`,
		`[{"kind":"action","runs":null}]`,
		`[{"kind":"action"}]`,
	} {
		var documents documentmodel.Outlines
		if err := json.Unmarshal([]byte(input), &documents); err == nil {
			t.Fatalf("accepted unsupported document/runtime: %s", input)
		}
	}
}
