package visit

import (
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

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
			Message: "this is test 1", Start: diagmodel.Position{
				Line:   1,
				Column: 2,
			}, Rule: "dummy name",
		},
		{
			Message: "this is test 2", Start: diagmodel.Position{
				Line:   3,
				Column: 4,
			}, Rule: "dummy name",
		},
	}
	if diff := cmp.Diff(errs, want, cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
		t.Error("unexpected errors from Errs() method:", diff)
	}
}

func TestRuleBaseErrsAreDetached(t *testing.T) {
	r := NewBase("rule", "description")
	r.Error(&workflowlang.Pos{Line: 1, Col: 1}, "message")
	r.Errs()[0].Path = "changed"
	if path := r.Errs()[0].Path; path != "" {
		t.Fatalf("reader changed a recorded finding: path %q", path)
	}
}

func TestRuleBaseAnnotateSince(t *testing.T) {
	r := NewBase("rule", "description")
	r.Error(&workflowlang.Pos{Line: 1, Col: 1}, "before")
	mark := r.FindingCount()
	r.Error(&workflowlang.Pos{Line: 2, Col: 1}, "after")
	r.AnnotateSince(mark, func(finding *findingmodel.Finding) { finding.Path = "annotated" })
	if count := r.FindingCount(); count != 2 {
		t.Fatalf("wanted 2 findings, have %d", count)
	}
	errs := r.Errs()
	if errs[0].Path != "" || errs[1].Path != "annotated" {
		t.Fatalf("annotation must cover only findings after the mark: %q, %q", errs[0].Path, errs[1].Path)
	}
	r.AnnotateSince(r.FindingCount()+1, func(*findingmodel.Finding) { t.Fatal("mark past the end annotates nothing") })
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
