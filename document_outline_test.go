package actionlint

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDocumentOutlinesRoundTrip(t *testing.T) {
	workflow, errs := Parse([]byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v7\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	action, err := ParseActionOutline("action.yml", []byte("name: Test\ndescription: Test action\nruns: {using: docker, image: 'docker://alpine:3'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := DocumentOutlines{workflowOutline("ci.yml", workflow, false), action}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got DocumentOutlines
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("document roundtrip changed data: %s", encoded)
	}
	for _, document := range got {
		previous := document.DocumentPath()
		moved := document.WithPath("new/" + previous)
		if moved.DocumentPath() != "new/"+previous || document.DocumentPath() != previous {
			t.Fatalf("WithPath mutated original or lost path: %#v", document)
		}
	}
}

func TestDocumentOutlinesRejectUnknownKinds(t *testing.T) {
	for _, input := range []string{
		`[null]`,
		`[{"kind":"unexpected"}]`,
		`[{"kind":"action","runs":{"kind":"unexpected"}}]`,
		`[{"kind":"action","runs":null}]`,
		`[{"kind":"action"}]`,
	} {
		var documents DocumentOutlines
		if err := json.Unmarshal([]byte(input), &documents); err == nil {
			t.Fatalf("accepted unsupported document/runtime: %s", input)
		}
	}
}
