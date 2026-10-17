package visit

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"

	"github.com/google/go-cmp/cmp"
)

func TestRuleBaseErrorfAndErrs(t *testing.T) {
	r := NewBase("dummy name", "dummy description")
	errs := r.Errs()
	if len(errs) > 0 {
		t.Error("no error is expected", errs)
	}
	r.Error(&workflowlang.Pos{Line: 1, Col: 2}, "this is test 1")
	r.Errorf(&workflowlang.Pos{Line: 3, Col: 4}, "this is test %d", 2)
	errs = r.Errs()
	if len(errs) != 2 {
		t.Error("wanted 2 errors but have", errs)
	}
	want := []*findingmodel.Finding{
		{
			Message: "this is test 1", Start: diagmodel.Position{Line: 1,
				Column: 2}, Rule: "dummy name",
		},
		{
			Message: "this is test 2", Start: diagmodel.Position{Line: 3,
				Column: 4}, Rule: "dummy name",
		},
	}
	if diff := cmp.Diff(errs, want, cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Error("unexpected errors from Errs() method:", diff)
	}
}

func TestRuleBaseNameAndDescription(t *testing.T) {
	r := NewBase("dummy name", "dummy description")
	if r.Name() != "dummy name" {
		t.Errorf("name is unexpected: %q", r.Name())
	}
	if r.Description() != "dummy description" {
		t.Errorf("description is unexpected: %q", r.Description())
	}
}
