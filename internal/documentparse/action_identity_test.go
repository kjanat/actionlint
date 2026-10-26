package documentparse

import (
	"testing"

	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
)

func TestCompositeAliasReferencesFollowStepOwners(t *testing.T) {
	content := []byte("name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - &shared\n      uses: $/nested\n    - *shared\n")
	document := Action("outer/action.yml", "outer/action.yml", content)
	if !document.CanAnalyze() || len(document.Findings) != 0 {
		t.Fatalf("invalid fixture: %+v", document.Findings)
	}
	workflow := document.Facts()
	steps := workflow.Jobs[""].Steps
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	refs := workflowfacts.Index("outer/action.yml", content, workflow, true)
	first, second := refs.Steps[steps[0]], refs.Steps[steps[1]]
	if first.Node == "" || second.Node == "" || first.Node == second.Node {
		t.Fatalf("aliased calls lost structural ownership: first=%+v second=%+v", first, second)
	}
	if first.Address != "runs.steps[0]" || second.Address != "runs.steps[1]" {
		t.Fatalf("incorrect owning addresses: %q, %q", first.Address, second.Address)
	}
	if steps[0].Pos.Line != steps[1].Pos.Line || steps[0].Pos.Col != steps[1].Pos.Col {
		t.Fatal("fixture must exercise distinct owners at shared source coordinates")
	}
	if first.Document != second.Document || first.Revision != second.Revision {
		t.Fatal("one source snapshot acquired multiple document identities")
	}
}
