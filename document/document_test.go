package document_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/document"
)

func TestReportsDecodeWithoutAnalysis(t *testing.T) {
	input := document.Outlines{
		document.Workflow{Path: "ci.yml", ParseStatus: "complete", Triggers: []string{"push"}, Jobs: []document.Job{{ID: "build", Needs: []string{}, Steps: []document.Step{{Kind: "uses", Uses: "./action", Reference: document.WorkspaceReference{Path: "action"}}}}}},
		document.Action{Path: "action.yml", ParseStatus: "complete", Inputs: []document.Input{}, Outputs: []document.Output{}, Runs: document.CompositeRuns{Steps: []document.Step{}}},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output document.Outlines
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, output) {
		t.Fatalf("round trip: %s", data)
	}
	if got := document.ParseWorkflowReference("./.github/workflows/reuse.yml"); got != (document.SelfRepositoryReference{Path: ".github/workflows/reuse.yml"}) {
		t.Fatal(got)
	}
}

func TestActionOutlinesWithoutRuntimeRoundTrip(t *testing.T) {
	input := document.Action{Path: "action.yml"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{string(data), `{"kind":"action","path":"action.yml"}`, `{"kind":"action","path":"action.yml","runs": null }`} {
		var action document.Action
		if err := json.Unmarshal([]byte(source), &action); err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if !reflect.DeepEqual(action, input) {
			t.Fatalf("round trip changed action: %+v", action)
		}
		var outlines document.Outlines
		if err := json.Unmarshal([]byte("["+source+"]"), &outlines); err != nil || len(outlines) != 1 || !reflect.DeepEqual(outlines[0], input) {
			t.Fatalf("outlines=%+v err=%v", outlines, err)
		}
	}
	for _, source := range []string{`{"kind":"action","runs":{}}`, `{"kind":"action","runs":{"kind":"typo"}}`} {
		var action document.Action
		if err := json.Unmarshal([]byte(source), &action); err == nil {
			t.Fatalf("accepted invalid runtime: %s", source)
		}
	}
}
