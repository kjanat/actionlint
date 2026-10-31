package document_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/document"
)

func TestReportsDecodeWithoutAnalysis(t *testing.T) {
	input := document.Outlines{
		document.NewWorkflow(document.WorkflowFields{Path: "ci.yml", ParseStatus: "complete", Triggers: []string{"push"}, Jobs: []document.Job{
			document.NewJob(document.JobFields{ID: "build", Needs: []string{}, Steps: []document.Step{document.NewStep(document.StepFields{Kind: "uses", Uses: "./action"})}}),
		}}),
		document.NewAction(document.ActionFields{Path: "action.yml", ParseStatus: "complete", Inputs: []document.Input{}, Outputs: []document.Output{}, Runs: document.NewCompositeRuns([]document.Step{})}),
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
	step := output[0].(document.Workflow).Jobs()[0].Steps()[0]
	if got := step.Reference(); got != (document.WorkspaceReference{Path: "action"}) {
		t.Fatalf("step reference = %#v", got)
	}
	if got := document.ParseWorkflowReference("./.github/workflows/reuse.yml"); got != (document.SelfRepositoryReference{Path: ".github/workflows/reuse.yml"}) {
		t.Fatal(got)
	}
}

func TestActionOutlinesWithoutRuntimeRoundTrip(t *testing.T) {
	input := document.NewAction(document.ActionFields{Path: "action.yml"})
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

func TestOutlineAccessorsReturnDetachedCopies(t *testing.T) {
	needs := []string{"lint"}
	workflow := document.NewWorkflow(document.WorkflowFields{Path: "ci.yml", Jobs: []document.Job{
		document.NewJob(document.JobFields{ID: "build", Needs: needs, Start: &diagnostic.Position{Line: 3, Column: 3}}),
	}})
	needs[0] = "changed"
	job := workflow.Jobs()[0]
	job.Needs()[0] = "mutated"
	job.Start().Line = 99
	if got := workflow.Jobs()[0]; got.Needs()[0] != "lint" || got.Start().Line != 3 {
		t.Fatalf("job storage leaked: needs=%v start=%v", got.Needs(), got.Start())
	}
	required := true
	action := document.NewAction(document.ActionFields{Path: "action.yml", Inputs: []document.Input{{ID: "token", Required: &required}}})
	*action.Inputs()[0].Required = false
	moved := action.WithPath("moved/action.yml").(document.Action)
	if !*moved.Inputs()[0].Required || action.DocumentPath() != "action.yml" || moved.DocumentPath() != "moved/action.yml" {
		t.Fatalf("action storage leaked: %+v", moved.Inputs())
	}
}
