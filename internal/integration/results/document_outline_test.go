package results_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"

	"actionlint.kjanat.dev/internal/report"
)

func TestDocumentOutlinesRoundTrip(t *testing.T) {
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{
		{Path: "ci.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v7\n")},
		{Path: "action.yml", Content: []byte("name: Test\ndescription: Test action\nruns: {using: docker, image: 'docker://alpine:3'}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := result.Documents

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got report.DocumentOutlines
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
