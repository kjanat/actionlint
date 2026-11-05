package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyJob reports a job that runs no steps. Reusable workflow calls have none.
func EmptyJob(job declaration.JobView) []reporting.Finding {
	if job.WorkflowCall().Present() || job.DeclaredUses().Present() {
		return nil
	}
	for range job.Steps() {
		return nil
	}
	return []reporting.Finding{reporting.Warning("empty-job", "", reporting.ViewPosition(job.Pos()), "job %q has no steps, so it does nothing", job.ID().Value())}
}
