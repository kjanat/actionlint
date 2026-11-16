package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyJob reports a job that runs no steps. Reusable workflow calls have none.
func EmptyJob(job declaration.Job) []reporting.StepFinding {
	if job.WorkflowCall != nil || job.DeclaredUses != nil {
		return nil
	}
	for range job.Steps() {
		return nil
	}
	return []reporting.StepFinding{{Rule: "empty-job", Severity: "error", Start: sourcepos.ZeroIfNil(job.Pos), Message: fmt.Sprintf("job %q must define at least one step or call a reusable workflow with uses", job.ID.Text())}}
}
