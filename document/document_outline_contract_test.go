package document_test

import (
	"encoding/json"
	"reflect"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"
)

func TestDocumentOutlinesRejectUnknownKinds(t *testing.T) {
	for _, input := range []string{
		`[null]`,
		`[{"kind":"unexpected"}]`,
		`[{"kind":"action","runs":{"kind":"unexpected"}}]`,
		`[{"kind":"action","runs":{}}]`,
		`[{"kind":"action","runs":[]}]`,
		`[{"kind":"action","runs":"node24"}]`,
	} {
		var documents documentmodel.Outlines
		if err := json.Unmarshal([]byte(input), &documents); err == nil {
			t.Fatalf("accepted unsupported document/runtime: %s", input)
		}
	}
}

func TestDocumentOutlinesAcceptMissingRuntime(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"null", `[{"kind":"action","path":"action.yml","runs":null}]`},
		{"absent", `[{"kind":"action","path":"action.yml"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var documents documentmodel.Outlines
			if err := json.Unmarshal([]byte(tc.input), &documents); err != nil {
				t.Fatal(err)
			}
			want := documentmodel.Outlines{documentmodel.Action{Path: "action.yml"}}
			if !reflect.DeepEqual(documents, want) {
				t.Fatalf("missing runtime changed action: got %#v, want %#v", documents, want)
			}
		})
	}
}
