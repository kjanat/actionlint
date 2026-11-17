package workflowvisit

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results/finding"

	"github.com/google/go-cmp/cmp"
)

func TestRuleBaseErrorfAndErrs(t *testing.T) {
	t.Parallel()
	r := NewBase("dummy name", "dummy description")
	errs := r.Errs()
	if len(errs) > 0 {
		t.Error("no error is expected", errs)
	}
	r.Error(&sourcetext.Position{Line: 1, Column: 2}, "this is test 1")
	r.Errorf(&sourcetext.Position{Line: 3, Column: 4}, "this is test %d", 2)
	errs = r.Errs()
	if len(errs) != 2 {
		t.Error("wanted 2 errors but have", errs)
	}
	want := []*finding.Finding{
		{
			Message: "this is test 1", Start: sourcetext.Position{
				Line:   1,
				Column: 2,
			}, Rule: "dummy name",
		},
		{
			Message: "this is test 2", Start: sourcetext.Position{
				Line:   3,
				Column: 4,
			}, Rule: "dummy name",
		},
	}
	if diff := cmp.Diff(errs, want, cmp.AllowUnexported(finding.Finding{})); diff != "" {
		t.Error("unexpected errors from Errs() method:", diff)
	}
}

func TestRuleBaseErrsAreDetached(t *testing.T) {
	t.Parallel()
	r := NewBase("rule", "description")
	r.Error(&sourcetext.Position{Line: 1, Column: 1}, "message")
	r.Errs()[0].Path = "changed"
	if path := r.Errs()[0].Path; path != "" {
		t.Fatalf("reader changed a recorded finding: path %q", path)
	}
}

func TestRuleBaseAnnotateSince(t *testing.T) {
	t.Parallel()
	r := NewBase("rule", "description")
	r.Error(&sourcetext.Position{Line: 1, Column: 1}, "before")
	mark := r.FindingCount()
	r.Error(&sourcetext.Position{Line: 2, Column: 1}, "after")
	r.AnnotateSince(mark, func(finding *finding.Finding) { finding.Path = "annotated" })
	if count := r.FindingCount(); count != 2 {
		t.Fatalf("wanted 2 findings, have %d", count)
	}
	errs := r.Errs()
	if errs[0].Path != "" || errs[1].Path != "annotated" {
		t.Fatalf("annotation must cover only findings after the mark: %q, %q", errs[0].Path, errs[1].Path)
	}
	r.AnnotateSince(r.FindingCount()+1, func(*finding.Finding) { t.Fatal("mark past the end annotates nothing") })
}

func TestRuleBaseNameAndDescription(t *testing.T) {
	t.Parallel()
	r := NewBase("dummy name", "dummy description")
	if r.Name() != "dummy name" {
		t.Errorf("name is unexpected: %q", r.Name())
	}
	if r.Description() != "dummy description" {
		t.Errorf("description is unexpected: %q", r.Description())
	}
}
