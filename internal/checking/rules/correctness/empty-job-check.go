package correctness

import (
	"fmt"

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
	return []reporting.Finding{{Rule: "empty-job", Severity: "error", Start: reporting.Position(reporting.ViewPosition(job.Pos())), Message: fmt.Sprintf("job %q must define at least one step or call a reusable workflow with uses", job.ID().Value())}}
}
