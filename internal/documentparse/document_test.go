package documentparse

import "testing"

func TestActionFactsDoNotBecomeWorkflowTraversalInputs(t *testing.T) {
	source := []byte("name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n")
	document := Action("action.yml", "/project/action.yml", source)
	if !document.CanAnalyze() || document.Metadata == nil || document.Workflow != nil {
		t.Fatalf("action entered workflow representation: %+v", document)
	}
	facts := document.Facts()
	if facts == nil || len(facts.Jobs[""].Steps) != 1 || document.Facts() != facts {
		t.Fatal("action facts are missing or unstable across consumers")
	}
	refs := References("/project/action.yml", source, document, nil)
	step := facts.Jobs[""].Steps[0]
	if refs["/project/action.yml"].Steps[step].Address != "runs.steps[0]" {
		t.Fatal("fact projections lost their action source identity")
	}
}
