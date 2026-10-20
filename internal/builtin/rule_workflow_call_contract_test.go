package builtin_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
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
			c := calls.NewCache("", "", nil)
			r := builtin.NewRuleWorkflowCall("", c)
			j := &workflowlang.Job{
				WorkflowCall: &workflowlang.WorkflowCall{
					Uses: &workflowlang.String{
						Value: tc.uses,
						Pos:   &workflowlang.Pos{},
					},
				},
			}
			err := r.VisitJobPre(j)
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

	c := calls.NewCache("", "", nil)
	r := builtin.NewRuleWorkflowCall("", c)

	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}

	if err := r.VisitJobPre(j); err != nil {
		t.Fatal(err)
	}
	errs := r.Errs()

	if len(errs) > 0 {
		t.Fatal("unexpected errors:", errs)
	}
}

func TestRuleWorkflowCallWriteEventNodeToMetadataCache(t *testing.T) {
	s := func(v string) *workflowlang.String {
		return &workflowlang.String{Value: v, Pos: &workflowlang.Pos{}}
	}
	w := &workflowlang.Workflow{
		On: []workflowlang.Event{
			&workflowlang.WorkflowCallEvent{
				Inputs: []*workflowlang.WorkflowCallEventInput{
					{
						Name: s("input1"),
						Type: workflowlang.WorkflowCallEventInputTypeString,
						ID:   "input1",
					},
				},
				Outputs: map[string]*workflowlang.WorkflowCallEventOutput{
					"output1": {Name: s("output1")},
				},
				Secrets: map[string]*workflowlang.WorkflowCallEventSecret{
					"secret1": {Name: s("secret1")},
				},
				Pos: &workflowlang.Pos{},
			},
		},
	}

	cwd := filepath.Join("path", "to", "project")
	c := calls.NewCache(cwd, cwd, nil)
	r := builtin.NewRuleWorkflowCall("test-workflow.yaml", c)

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
			"input1": {Name: "input1", Required: false, Type: exprlang.StringType{}},
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
