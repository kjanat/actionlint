package application

import (
	"path/filepath"
	"slices"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type mutateCanonicalCallRule struct {
	workflowvisit.Base
	visits    int
	copied    string
	unchanged bool
}

func (rule *mutateCanonicalCallRule) VisitWorkflowPre(workflow workflowdecl.WorkflowView) error {
	rule.visits++
	job, _ := workflow.Job("test")
	for step := range job.Steps() {
		if action, ok := step.Action(); ok && action.Uses().Value() == "./local" {
			rule.copied = action.Uses().Value()
			rule.copied = "./decoy"
			rule.unchanged = action.Uses().Value() == "./local"
		}
	}
	return nil
}

func TestExecutableBitUsesOriginalCompositeSourceWithImmutableCallback(t *testing.T) {
	root, _ := executableFixture(t)
	original := writeShellcheckFixture(t, root, "local/action.yml", `name: original
description: canonical source
runs:
  using: composite
  steps:
    - shell: bash
      run: ./bad.sh
`)
	decoy := writeShellcheckFixture(t, root, "decoy/action.yml", `name: decoy
description: mutated callback target
runs:
  using: composite
  steps:
    - shell: bash
      run: ./good.sh
`)
	steps := "- uses: actions/checkout@v6\n- uses: ./local"
	baseline := compositeAnalysis(t, root, steps, analysisRequest{})
	mutator := &mutateCanonicalCallRule{Base: workflowvisit.NewBase("mutate-call", "")}
	mutated := compositeAnalysis(t, root, steps, analysisRequest{OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
		return append([]workflowvisit.Rule{mutator}, rules...)
	}})
	if mutator.visits != 1 || !mutator.unchanged || mutator.copied != "./decoy" {
		t.Fatalf("workflow callback changed its immutable declaration or did not run: %d", mutator.visits)
	}
	findings := func(result *analysisResult) []diagmodel.Diagnostic {
		return slices.DeleteFunc(slices.Clone(result.Diagnostics), func(diagnostic diagmodel.Diagnostic) bool {
			return diagnostic.Rule != "executable-bit"
		})
	}
	base, got := findings(baseline), findings(mutated)
	if len(base) != 1 || len(got) != 1 || base[0].Message != got[0].Message || base[0].Start != got[0].Start || filepath.Join(root, got[0].Path) != original {
		t.Fatalf("executable-bit source changed after callback mutation: baseline=%+v mutated=%+v", base, got)
	}
	if !slices.Contains(mutated.Inputs, original) {
		t.Fatalf("canonical composite source absent from analysis inputs: %v", mutated.Inputs)
	}
	if slices.Contains(mutated.Inputs, decoy) {
		t.Fatalf("detached callback value changed the validation target: %v", mutated.Inputs)
	}
}
