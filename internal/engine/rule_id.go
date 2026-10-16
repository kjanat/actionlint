package engine

import (
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/checks/identifier"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleID adapts a complete job scope. Step order and duplicate detection are
// explicit inputs; the checker does not depend on preceding visitor callbacks.
type RuleID struct{ RuleBase }

func NewRuleID() *RuleID { return &RuleID{RuleBase: builtinRuleBase("id")} }
func (rule *RuleID) VisitJobPre(job *workflowlang.Job) error {
	name := func(value *workflowlang.String) identifier.Name {
		return identifier.Name{Value: value.Value, Dynamic: value.ContainsExpression(), Start: factPosition(value.Pos)}
	}
	input := identifier.Job{}
	if job.ID != nil {
		input.ID = new(name(job.ID))
	}
	for _, value := range job.Needs {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[diagnostic.Position]*workflowlang.Step{}
	var collect func([]*workflowlang.Step)
	collect = func(steps []*workflowlang.Step) {
		for _, step := range steps {
			if step.ID != nil {
				input.Steps = append(input.Steps, name(step.ID))
				owners[factPosition(step.ID.Pos)] = step
			}
			if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
				collect(parallel.Steps)
			}
		}
	}
	collect(job.Steps)
	for _, d := range identifier.Check(input) {
		finding := legacyDiagnosticError(d)
		if step := owners[d.Start]; step != nil {
			jobID := outlineString(job.ID)
			finding.owner = &diagnosticOwner{Job: jobID, Step: step.Pos}
			finding.ignoreOrigin = newDiagnosticIgnoreOrigin(jobID, step)
		}
		rule.errs = append(rule.errs, finding)
	}
	return nil
}
