package engine

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDocumentOutlinesRoundTrip(t *testing.T) {
	workflow, errs := Parse([]byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
`))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	action, err := ParseActionOutline("action.yml", []byte(`name: Test
description: Test action
runs: {using: docker, image: 'docker://alpine:3'}
`))
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
		`[{"kind":"action","runs":{}}]`,
		`[{"kind":"action","runs":[]}]`,
		`[{"kind":"action","runs":"node24"}]`,
	} {
		var documents DocumentOutlines
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
			var documents DocumentOutlines
			if err := json.Unmarshal([]byte(tc.input), &documents); err != nil {
				t.Fatal(err)
			}
			want := DocumentOutlines{ActionOutline{Path: "action.yml"}}
			if !reflect.DeepEqual(documents, want) {
				t.Fatalf("missing runtime changed action: got %#v, want %#v", documents, want)
			}
		})
	}
}
