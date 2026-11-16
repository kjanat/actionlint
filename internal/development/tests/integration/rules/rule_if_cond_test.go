package rules

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestRuleIfCond(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cond  string
		valid bool
	}{
		{"", true},
		{"true", false},
		{"false", false},
		{"true || false", false},
		{"github.ref_name == 'foo'", true},
		{"${{ true }}", false},
		{"${{ contains('foo', 'f') }}", false},
		{"${{ github.ref_name == 'foo' }}", true},
		{"${{ foo }}", true},
		{"${{ foo }}\n", false},
		{"${{ foo }} ", false},
		{" ${{ foo }}", false},
		{"${{ true }} && ${{ true }}", false},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q at step", tc.cond), func(t *testing.T) {
			t.Parallel()
			var s definition.Step
			if len(tc.cond) > 0 {
				s.If = &definition.String{Value: tc.cond, Pos: &sourcetext.Position{}}
			}

			r := correctness.IfCondRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
			if err := r.VisitStep(declarations.Step(&s)); err != nil {
				t.Fatal(err)
			}

			errs := r.Errs()
			if tc.valid && len(errs) > 0 {
				t.Fatalf("wanted no error but have %q for condition %q", errs, tc.cond)
			}
			if !tc.valid && len(errs) != 1 {
				t.Fatalf("wanted one error but have %q for condition %q", errs, tc.cond)
			}
		})
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q at job", tc.cond), func(t *testing.T) {
			t.Parallel()
			var j definition.Job
			if len(tc.cond) > 0 {
				j.If = &definition.String{Value: tc.cond, Pos: &sourcetext.Position{}}
				j.Snapshot = &definition.Snapshot{
					ImageName: &definition.String{Value: "test", Pos: &sourcetext.Position{}},
					If:        &definition.String{Value: tc.cond, Pos: &sourcetext.Position{}},
				}
			}

			r := correctness.IfCondRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
			if err := r.VisitJobPre(declarations.Job(&j)); err != nil {
				t.Fatal(err)
			}

			errs := r.Errs()
			if tc.valid && len(errs) > 0 {
				t.Fatalf("wanted no error but have %q for condition %q", errs, tc.cond)
			}
			if !tc.valid && len(errs) != 2 {
				t.Fatalf("wanted one error but have %q for condition %q", errs, tc.cond)
			}
		})
	}
}
