package rules_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// workflowCallVisitor exercises the production adapter with prepared workflow inputs.
func workflowCallVisitor(tb testing.TB, workflow declaration.WorkflowView, cache *workflowinputs.Cache, facts repository.Snapshot) workflowvisit.Rule {
	tb.Helper()
	var uses []string
	for _, job := range workflow.Jobs() {
		if call := job.WorkflowCall(); call.Present() && call.Uses().Present() {
			uses = append(uses, call.Uses().Value())
		}
	}
	if err := cache.LoadCalls(tb.Context(), uses); err != nil {
		tb.Fatal(err)
	}
	return declarationchecks.New(checking.ConfigurationRules(), correctness.WorkflowCallCheck(workflow, cache.Resolved(), facts, nil))
}
