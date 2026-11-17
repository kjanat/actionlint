package rules

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
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
			job      *definition.Job
			expected string
		}{
			{
				"jobs.<job_id>",
				&definition.Job{
					ID: &definition.String{Value: input, Pos: &sourcetext.Position{}},
				},
				"invalid job ID",
			},
			{
				"jobs.<job_id>.needs",
				&definition.Job{
					ID: &definition.String{Value: "test", Pos: &sourcetext.Position{}},
					Needs: []*definition.String{
						{Value: input, Pos: &sourcetext.Position{}},
					},
				},
				"invalid job ID",
			},
			{
				"steps.*.id",
				&definition.Job{
					Steps: []*definition.Step{
						{ID: &definition.String{Value: input, Pos: &sourcetext.Position{}}},
					},
				},
				"invalid step ID",
			},
		}

		for _, tc := range tests {
			t.Run(input+"/"+tc.where, func(t *testing.T) {
				t.Parallel()
				r := correctness.IdRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
				if err := r.VisitJobPre(declarations.Job(tc.job)); err != nil {
					t.Fatal(err)
				}
				for _, s := range tc.job.Steps {
					if err := r.VisitStep(declarations.Step(s)); err != nil {
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
			job := &definition.Job{
				ID: &definition.String{Value: input, Pos: &sourcetext.Position{}},
				Needs: []*definition.String{
					{Value: input, Pos: &sourcetext.Position{}},
				},
				Steps: []*definition.Step{
					{ID: &definition.String{Value: stepID, Pos: &sourcetext.Position{}}},
				},
			}
			r := correctness.IdRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
			if err := r.VisitJobPre(declarations.Job(job)); err != nil {
				t.Fatal(err)
			}
			for _, s := range job.Steps {
				if err := r.VisitStep(declarations.Step(s)); err != nil {
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
