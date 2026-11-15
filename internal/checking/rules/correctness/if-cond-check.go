package correctness

import (
	condition "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/if-cond"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func checkCondition(value *workflowlang.String) []reporting.Finding {
	if value == nil {
		return nil
	}
	return reporting.FromDiagnostics(condition.Check(value.Value, reporting.Position(value.Pos)))
}

func checkJobConditions(job declaration.Job) []reporting.Finding {
	out := checkCondition(job.If)
	if job.Snapshot != nil {
		out = append(out, checkCondition(job.Snapshot.If)...)
	}
	return out
}

func checkStepCondition(step declaration.Step) []reporting.Finding {
	return checkCondition(step.If)
}
