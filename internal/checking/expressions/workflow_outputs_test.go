package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func TestWorkflowOutputReferencesUseCalledWorkflowDeclarations(t *testing.T) {
	t.Parallel()
	const source = `on:
  workflow_call:
    outputs:
      published:
        value: ${{ jobs.call.outputs.missing }}
jobs:
  call:
    uses: ./.github/workflows/callee.yml
`
	checker, workflow := newTestCheckerForSource(t, source)
	lookups := 0
	checker.dependencies.Workflow = func(reference string) (*calls.Metadata, error) {
		if reference != "./.github/workflows/callee.yml" {
			t.Fatalf("unexpected lookup: %s", reference)
		}
		lookups++
		return &calls.Metadata{Outputs: calls.Outputs{"artifact": {Name: "artifact"}}}, nil
	}
	if err := checker.VisitWorkflowPre(workflow); err != nil {
		t.Fatal(err)
	}
	if err := checker.VisitWorkflowPost(workflow); err != nil {
		t.Fatal(err)
	}
	findings := checker.Findings()
	if len(findings) != 1 || findings[0].Rule != "expression-reference" || findings[0].Code != "unknown-property" {
		t.Fatalf("called workflow output declarations were not checked: %+v", findings)
	}
	if lookups != 1 {
		t.Fatalf("called workflow resolved %d times", lookups)
	}
}
