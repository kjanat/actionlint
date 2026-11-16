package rules_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// workflowCallVisitor exercises the production adapter with prepared workflow inputs.
func workflowCallVisitor(tb testing.TB, workflow workflows.Workflow, cache *workflowinputs.Cache, facts repository.Snapshot) workflowvisit.Rule {
	tb.Helper()
	var uses []string
	for _, job := range workflow.Jobs() {
		if call := job.WorkflowCall; call != nil && call.Uses != nil {
			uses = append(uses, call.Uses.Text())
		}
	}
	if err := cache.LoadCalls(tb.Context(), uses); err != nil {
		tb.Fatal(err)
	}
	return define.NewChecksVisitor(checking.ConfigurationRules(), correctness.WorkflowCallRule.Name, correctness.WorkflowCallChecks(workflow, cache.Resolved(), facts, nil))
}
