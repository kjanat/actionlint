package engine

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestCheckInvalidJobNames(t *testing.T) {
	// Note: Empty string is already reported as error by parser
	inputs := []string{
		"-foo",
		"v1.2.3",
		"hello!",
		"じょぶ",
		"12345",
	}

	for _, input := range inputs {
		tests := []struct {
			where    string
			job      *workflowlang.Job
			expected string
		}{
			{
				"jobs.<job_id>",
				&workflowlang.Job{
					ID: &workflowlang.String{Value: input, Pos: &workflowlang.Pos{}},
				},
				"invalid job ID",
			},
			{
				"jobs.<job_id>.needs",
				&workflowlang.Job{
					ID: &workflowlang.String{Value: "test", Pos: &workflowlang.Pos{}},
					Needs: []*workflowlang.String{
						{Value: input, Pos: &workflowlang.Pos{}},
					},
				},
				"invalid job ID",
			},
			{
				"steps.*.id",
				&workflowlang.Job{
					Steps: []*workflowlang.Step{
						{ID: &workflowlang.String{Value: input, Pos: &workflowlang.Pos{}}},
					},
				},
				"invalid step ID",
			},
		}

		for _, tc := range tests {
			t.Run(input+"/"+tc.where, func(t *testing.T) {
				r := NewRuleID()
				if err := r.VisitJobPre(tc.job); err != nil {
					t.Fatal(err)
				}
				for _, s := range tc.job.Steps {
					if err := r.VisitStep(s); err != nil {
						t.Fatal(err)
					}
				}
				errs := r.Errs()
				if len(errs) != 1 {
					t.Fatalf("Wanted exactly one error but got %d errors: %v", len(errs), errs)
				}
				msg := errs[0].String()
				if !strings.Contains(msg, tc.expected) {
					t.Errorf("Error message %q should contain %q", msg, tc.expected)
				}
			})
		}
	}
}

func TestCheckValidJobNames(t *testing.T) {
	inputs := []string{
		"foo-bar",
		"foo_bar",
		"foo--bar",
		"foo__bar",
		"_FOO123-",
		"abcdefhijklmnopqrstuvwxyzABCDEFHIJKLMNOPQRSTUVWXYZ",
		"_____",
		"_-_-_",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			stepID := input
			if strings.HasPrefix(stepID, "__") {
				stepID = "step" + stepID
			}
			job := &workflowlang.Job{
				ID: &workflowlang.String{Value: input, Pos: &workflowlang.Pos{}},
				Needs: []*workflowlang.String{
					{Value: input, Pos: &workflowlang.Pos{}},
				},
				Steps: []*workflowlang.Step{
					{ID: &workflowlang.String{Value: stepID, Pos: &workflowlang.Pos{}}},
				},
			}
			r := NewRuleID()
			if err := r.VisitJobPre(job); err != nil {
				t.Fatal(err)
			}
			for _, s := range job.Steps {
				if err := r.VisitStep(s); err != nil {
					t.Fatal(err)
				}
			}
			errs := r.Errs()
			if len(errs) > 0 {
				t.Fatalf("Unexpected error(s): %#v", errs)
			}
		})
	}
}
