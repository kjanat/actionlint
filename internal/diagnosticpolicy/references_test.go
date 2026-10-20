package diagnosticpolicy

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

func TestReferenceOwnershipIsIndependentOfSuppressionOrigin(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        run: echo ok\n      - *shared\n")
	workflow, parseErrors := workflowlang.Parse(content)
	if len(parseErrors) != 0 {
		t.Fatal(parseErrors)
	}
	first, second := workflow.Jobs["test"].Steps[0], workflow.Jobs["test"].Steps[1]
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
			finding := &findingmodel.Finding{Start: diagnostic.Position{Line: first.Pos.Line, Column: first.Pos.Col}, Owner: tc.owner,
				IgnoreOrigin: findingmodel.StepOrigin("test", second), RawScriptSource: tc.raw, Reference: tc.reference}
			InferReferences([]*findingmodel.Finding{finding}, "ci.yml", map[string]workflowfacts.References{"ci.yml": refs})
			if finding.Reference == nil || *finding.Reference != tc.want {
				t.Fatalf("reference=%+v want=%+v", finding.Reference, tc.want)
			}
		})
	}
}
