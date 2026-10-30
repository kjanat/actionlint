package builtin_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"

	"github.com/google/go-cmp/cmp"
)

func TestRuleWorkflowCallCheckWorkflowCallUsesFormat(t *testing.T) {
	tests := []struct {
		uses string
		ok   bool
	}{
		{"owner/repo/x.yml@ref", true},
		{"owner/repo/x.yml@@", true},
		{"owner/repo/x.yml@release/v1", true},
		{"./path/to/x.yml", true},
		{"$/path/to/x.yml", true},
		{"$//path/to/x.yml", true},
		{"${{ env.FOO }}", true},
		{"./path/to/x.yml@ref", false},
		{"$/path/to/x.yml@ref", false},
		{"/path/to/x.yml@ref", false},
		{"./", false},
		{"$/", false},
		{"$//", false},
		{"$///", false},
		{".", false},
		{"owner/x.yml@ref", false},
		{"owner/repo@ref", false},
		{"owner/repo/x.yml", false},
		{"/repo/x.yml@ref", false},
		{"owner//x.yml@ref", false},
		{"owner/repo/@ref", false},
		{"owner/repo/x.yml@", false},
	}

	for _, tc := range tests {
		t.Run(tc.uses, func(t *testing.T) {
			c := newCapturedWorkflowCache(t, "", "", nil)
			r := builtin.NewRuleWorkflowCall(nil, c)
			j := &workflowlang.Job{
				WorkflowCall: &workflowlang.WorkflowCall{
					Uses: &workflowlang.String{
						Value: tc.uses,
						Pos:   &workflowlang.Pos{},
					},
				},
			}
			err := r.VisitJobPre(ownedJobFixture(j))
			if err != nil {
				t.Fatal(err)
			}
			errs := r.Errs()
			if tc.ok && len(errs) > 0 {
				t.Fatalf("Error occurred: %v", errs)
			}
			if !tc.ok {
				if len(errs) > 2 || len(errs) == 0 {
					t.Fatalf("Wanted one error but have: %v", errs)
				}
			}
		})
	}
}

func TestRuleWorkflowCallNestedWorkflowCalls(t *testing.T) {
	w := &workflowlang.Workflow{
		On: []workflowlang.Event{
			&workflowlang.WorkflowCallEvent{
				Pos: &workflowlang.Pos{},
			},
		},
	}

	j := &workflowlang.Job{
		WorkflowCall: &workflowlang.WorkflowCall{
			Uses: &workflowlang.String{
				Value: "o/r/w.yaml@r",
				Pos:   &workflowlang.Pos{},
			},
		},
	}

	c := newCapturedWorkflowCache(t, "", "", nil)
	r := builtin.NewRuleWorkflowCall(nil, c)

	if err := r.VisitWorkflowPre(ownedWorkflowFixture(w)); err != nil {
		t.Fatal(err)
	}

	if err := r.VisitJobPre(ownedJobFixture(j)); err != nil {
		t.Fatal(err)
	}
	errs := r.Errs()

	if len(errs) > 0 {
		t.Fatal("unexpected errors:", errs)
	}
}

func TestRuleWorkflowCallWriteEventNodeToMetadataCache(t *testing.T) {
	cwd := filepath.Join(sourceFixtureBase(t), "path", "to", "project")
	path := filepath.Join(cwd, "test-workflow.yaml")
	content := []byte("on:\n  workflow_call:\n    inputs:\n      input1: {type: string}\n    outputs:\n      output1: {value: '${{ jobs.test.outputs.value }}'}\n    secrets:\n      secret1: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	declarations := newCapturedSnapshots(t, cwd)
	parsed := documentparse.Workflow(path, content)
	snapshot, err := declarations.Document(path, content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	w, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("captured workflow unavailable")
	}
	c := calls.NewCache(cwd, cwd, nil)
	c.SetSources(t.Context(), declarations.Sources.Store, cwd)
	r := builtin.NewRuleWorkflowCall(snapshot, c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	errs := r.Errs()
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	m, _, ok := c.Lookup("./test-workflow.yaml")
	if !ok {
		t.Fatal("no metadata was created")
	}

	want := &calls.Metadata{
		Inputs: calls.Inputs{
			"input1": {Name: "input1", Required: false, Type: calls.InputTypeString},
		},
		Outputs: calls.Outputs{
			"output1": {Name: "output1"},
		},
		Secrets: calls.Secrets{
			"secret1": {Name: "secret1", Required: false},
		},
	}

	if diff := cmp.Diff(want, m); diff != "" {
		t.Fatal(diff)
	}
}
