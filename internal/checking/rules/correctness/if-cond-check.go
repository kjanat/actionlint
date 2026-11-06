package correctness

import (
	condition "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/if-cond"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkCondition(value declaration.StringView) []reporting.Finding {
	if !value.Present() {
		return nil
	}
	return reporting.FromDiagnostics(condition.Check(value.Value(), reporting.Position(reporting.ViewPosition(value.Pos()))))
}

func checkJobConditions(job declaration.JobView) []reporting.Finding {
	out := checkCondition(job.If())
	if job.Snapshot().Present() {
		out = append(out, checkCondition(job.Snapshot().If())...)
	}
	return out
}

func checkStepCondition(step declaration.StepView) []reporting.Finding {
	return checkCondition(step.If())
}
