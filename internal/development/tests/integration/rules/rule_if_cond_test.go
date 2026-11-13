package rules

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
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
			var s workflowlang.Step
			if len(tc.cond) > 0 {
				s.If = &workflowlang.String{Value: tc.cond, Pos: &workflowlang.Pos{}}
			}

			r := declarationchecks.New(checking.ConfigurationRules(), correctness.IfCondCheck())
			if err := r.VisitStep(fixturedecl.Step(&s)); err != nil {
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
			var j workflowlang.Job
			if len(tc.cond) > 0 {
				j.If = &workflowlang.String{Value: tc.cond, Pos: &workflowlang.Pos{}}
				j.Snapshot = &workflowlang.Snapshot{
					ImageName: &workflowlang.String{Value: "test", Pos: &workflowlang.Pos{}},
					If:        &workflowlang.String{Value: tc.cond, Pos: &workflowlang.Pos{}},
				}
			}

			r := declarationchecks.New(checking.ConfigurationRules(), correctness.IfCondCheck())
			if err := r.VisitJobPre(fixturedecl.Job(&j)); err != nil {
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
