package policy

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestReferenceOwnershipIsIndependentOfSuppressionOrigin(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        run: echo ok\n      - *shared\n")
	workflow, parseErrors := parser.ParseWorkflow(content)
	if len(parseErrors) != 0 {
		t.Fatal(parseErrors)
	}
	first, second := workflow.Jobs["test"].Steps[0], workflow.Jobs["test"].Steps[1]
	bound := workflows.Bind("ci.yml", string(content), workflow, false, "")
	job, present := bound.Workflow().Job("test")
	if !present {
		t.Fatal("owned job unavailable")
	}
	origin := slices.Collect(job.Steps())[1].Origin("test")
	if !origin.HasPosition {
		t.Fatal("owned suppression position unavailable")
	}
	refs := facts.Index("ci.yml", content, workflow, false)
	firstRef := refs.Positions[[2]int{first.Pos.Line, first.Pos.Column}]
	secondRef := refs.Positions[[2]int{second.Pos.Line, second.Pos.Column}]
	explicit := sourcetext.Reference{Document: "provider", Node: "explicit"}
	for _, tc := range []struct {
		name      string
		owner     *finding.Owner
		reference *sourcetext.Reference
		raw       bool
		want      sourcetext.Reference
	}{
		{name: "explicit owner", owner: &finding.Owner{Job: "test", Step: second.Pos}, want: secondRef},
		{name: "ignore alone", want: firstRef},
		{name: "raw source", owner: &finding.Owner{Job: "test", Step: second.Pos}, raw: true, want: firstRef},
		{name: "provider reference", owner: &finding.Owner{Job: "test", Step: second.Pos}, reference: &explicit, want: explicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := &finding.Finding{
				Start: *first.Pos, Owner: tc.owner,
				IgnoreOrigin: &finding.IgnoreOrigin{Job: origin.Job, StepID: origin.StepID, StepName: origin.StepName, Uses: origin.Uses, StepPosition: new(origin.Position)}, Source: results.DiagnosticSource{RawScript: tc.raw}, Reference: tc.reference,
			}
			InferReferences([]*finding.Finding{item}, "ci.yml", map[string]facts.SourceIndex{"ci.yml": refs.SourceIndex()})
			if item.Reference == nil || *item.Reference != tc.want {
				t.Fatalf("reference=%+v want=%+v", item.Reference, tc.want)
			}
		})
	}
}

func TestParsedMixedCaseJobReference(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
	workflow, findings := parser.ParseWorkflow(content)
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	job := workflow.Jobs["build"]
	if job == nil || job.ID == nil || job.ID.Value != "Build" {
		t.Fatalf("parsed job identity missing: %+v", workflow.Jobs)
	}
	refs := facts.Index("ci.yml", content, workflow, false)
	item := &finding.Finding{
		Start: sourcetext.Position{Line: 1, Column: 1},
		Owner: &finding.Owner{Job: job.ID.Value},
	}
	InferReferences([]*finding.Finding{item}, "ci.yml", map[string]facts.SourceIndex{"ci.yml": refs.SourceIndex()})
	want := refs.Jobs["build"]
	if want.Node == "" || item.Reference == nil || *item.Reference != want {
		t.Fatalf("mixed-case job reference = %+v, want %+v", item.Reference, want)
	}
}

func TestReferenceInferenceRequiresPathOwnership(t *testing.T) {
	t.Parallel()
	references := facts.References{Document: sourcetext.Reference{Document: "entry", Revision: "revision", Node: "document"}}
	foreign := &finding.Finding{Path: "unrelated.sh"}
	alias := &finding.Finding{Path: "entry-alias.yml"}
	InferReferences([]*finding.Finding{foreign, alias}, "entry.yml", map[string]facts.SourceIndex{"entry.yml": references.SourceIndex()}, "entry-alias.yml")
	if foreign.Reference != nil || alias.Reference == nil || *alias.Reference != references.Document {
		t.Fatalf("path ownership inference changed: foreign=%+v alias=%+v", foreign.Reference, alias.Reference)
	}
}
