package rules

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCheckInvalidJobNames(t *testing.T) {
	t.Parallel()
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
				t.Parallel()
				r := declarationchecks.New(checking.ConfigurationRules(), correctness.IdCheck())
				if err := r.VisitJobPre(fixturedecl.Job(tc.job)); err != nil {
					t.Fatal(err)
				}
				for _, s := range tc.job.Steps {
					if err := r.VisitStep(fixturedecl.Step(s)); err != nil {
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
	t.Parallel()
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
			t.Parallel()
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
			r := declarationchecks.New(checking.ConfigurationRules(), correctness.IdCheck())
			if err := r.VisitJobPre(fixturedecl.Job(job)); err != nil {
				t.Fatal(err)
			}
			for _, s := range job.Steps {
				if err := r.VisitStep(fixturedecl.Step(s)); err != nil {
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
