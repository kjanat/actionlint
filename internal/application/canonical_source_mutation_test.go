package application

import (
	"path/filepath"
	"slices"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type mutateCanonicalCallRule struct {
	workflowvisit.Base
	visits int
}

func (rule *mutateCanonicalCallRule) VisitWorkflowPre(workflow *workflowlang.Workflow) error {
	rule.visits++
	for _, step := range workflow.Jobs["test"].Steps {
		if action, ok := step.Exec.(*workflowlang.ExecAction); ok && action.Uses.Value == "./local" {
			action.Uses.Value = "./decoy"
		}
	}
	return nil
}

func TestExecutableBitUsesOriginalCompositeSourceAfterLegacyCallbackMutation(t *testing.T) {
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
	if mutator.visits != 1 {
		t.Fatalf("workflow callback did not mutate legacy call: %d", mutator.visits)
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
	if !slices.Contains(mutated.Inputs, decoy) {
		t.Fatalf("legacy callback target was not observed by validation: %v", mutated.Inputs)
	}
}
