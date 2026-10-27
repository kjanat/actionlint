package diagnosticpolicy

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/source"
)

func TestReferenceOwnershipIsIndependentOfSuppressionOrigin(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        run: echo ok\n      - *shared\n")
	workflow, parseErrors := workflowparser.Parse(content)
	if len(parseErrors) != 0 {
		t.Fatal(parseErrors)
	}
	first, second := workflow.Jobs["test"].Steps[0], workflow.Jobs["test"].Steps[1]
	bound := workflowdecl.Publish(string(content), workflow, false, "").Bind("ci.yml")
	job, present := bound.Workflow().Job("test")
	if !present {
		t.Fatal("owned job unavailable")
	}
	origin := slices.Collect(job.Steps())[1].Origin("test")
	if !origin.HasPosition {
		t.Fatal("owned suppression position unavailable")
	}
	refs := workflowfacts.Index("ci.yml", content, workflow, false)
	firstRef := refs.Positions[[2]int{first.Pos.Line, first.Pos.Col}]
	secondRef := refs.Positions[[2]int{second.Pos.Line, second.Pos.Col}]
	explicit := source.Reference{Document: "provider", Node: "explicit"}
	for _, tc := range []struct {
		name      string
		owner     *findingmodel.Owner
		reference *source.Reference
		raw       bool
		want      source.Reference
	}{
		{name: "explicit owner", owner: &findingmodel.Owner{Job: "test", Step: second.Pos}, want: secondRef},
		{name: "ignore alone", want: firstRef},
		{name: "raw source", owner: &findingmodel.Owner{Job: "test", Step: second.Pos}, raw: true, want: firstRef},
		{name: "provider reference", owner: &findingmodel.Owner{Job: "test", Step: second.Pos}, reference: &explicit, want: explicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finding := &findingmodel.Finding{
				Start: diagnostic.Position{Line: first.Pos.Line, Column: first.Pos.Col}, Owner: tc.owner,
				IgnoreOrigin: &findingmodel.IgnoreOrigin{Job: origin.Job, StepID: origin.StepID, StepName: origin.StepName, Uses: origin.Uses, StepPosition: new(origin.Position)}, RawScriptSource: tc.raw, Reference: tc.reference,
			}
			InferReferences([]*findingmodel.Finding{finding}, "ci.yml", map[string]workflowfacts.SourceIndex{"ci.yml": refs.SourceIndex()})
			if finding.Reference == nil || *finding.Reference != tc.want {
				t.Fatalf("reference=%+v want=%+v", finding.Reference, tc.want)
			}
		})
	}
}

func TestParsedMixedCaseJobReference(t *testing.T) {
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
	workflow, findings := workflowparser.Parse(content)
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	job := workflow.Jobs["build"]
	if job == nil || job.ID == nil || job.ID.Value != "Build" {
		t.Fatalf("parsed job identity missing: %+v", workflow.Jobs)
	}
	refs := workflowfacts.Index("ci.yml", content, workflow, false)
	finding := &findingmodel.Finding{
		Start: diagnostic.Position{Line: 1, Column: 1},
		Owner: &findingmodel.Owner{Job: job.ID.Value},
	}
	InferReferences([]*findingmodel.Finding{finding}, "ci.yml", map[string]workflowfacts.SourceIndex{"ci.yml": refs.SourceIndex()})
	want := refs.Jobs["build"]
	if want.Node == "" || finding.Reference == nil || *finding.Reference != want {
		t.Fatalf("mixed-case job reference = %+v, want %+v", finding.Reference, want)
	}
}

func TestReferenceInferenceRequiresPathOwnership(t *testing.T) {
	references := workflowfacts.References{Document: source.Reference{Document: "entry", Revision: "revision", Node: "document"}}
	foreign := &findingmodel.Finding{Path: "unrelated.sh"}
	alias := &findingmodel.Finding{Path: "entry-alias.yml"}
	InferReferences([]*findingmodel.Finding{foreign, alias}, "entry.yml", map[string]workflowfacts.SourceIndex{"entry.yml": references.SourceIndex()}, "entry-alias.yml")
	if foreign.Reference != nil || alias.Reference == nil || *alias.Reference != references.Document {
		t.Fatalf("path ownership inference changed: foreign=%+v alias=%+v", foreign.Reference, alias.Reference)
	}
}
