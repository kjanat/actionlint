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
