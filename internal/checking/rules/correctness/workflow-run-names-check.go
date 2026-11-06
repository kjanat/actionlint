package correctness

import (
	namecheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-run-names"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflownames "actionlint.kjanat.dev/v2/internal/input/files/names"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type workflowRunNamesRule struct {
	workflowvisit.Base
	enabled bool
	root    string
	index   *workflownames.Index
}

// NewRuleWorkflowRunNames checks trigger filters against names loaded for root.
func NewRuleWorkflowRunNames(root string, index *workflownames.Index, enabled bool) workflowvisit.Rule {
	descriptor := WorkflowRunNamesDeclaration()
	return &workflowRunNamesRule{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), root: root, index: index, enabled: enabled}
}

func (r *workflowRunNamesRule) VisitWorkflowPre(w workflowdecl.WorkflowView) error {
	if !r.enabled {
		return nil
	}
	references := namecheck.WorkflowRunFilters(w)
	if len(references) == 0 {
		return nil
	}
	names, err := r.index.ForRoot(r.root)
	if err != nil {
		return err
	}
	for _, d := range namecheck.Check(names, references) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}
