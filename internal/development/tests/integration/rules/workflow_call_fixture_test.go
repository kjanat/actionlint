package rules_test

import (
	checking "actionlint.kjanat.dev/v2/internal/checking"
	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// workflowCallVisitor exercises the production adapter with prepared workflow inputs.
func workflowCallVisitor(workflow declaration.WorkflowView, cache *calls.Cache, facts repository.Snapshot) workflowvisit.Rule {
	return declarationchecks.New(checking.ConfigurationRules(), correctness.WorkflowCallCheck(workflow, cache, facts, nil))
}
