package correctness

import (
	"slices"

	jobneeds "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/job-needs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkJobDependencies(w declaration.WorkflowView) []reporting.Finding {
	jobs := slices.Collect(func(yield func(declaration.JobView) bool) {
		for _, job := range w.Jobs() {
			if !yield(job) {
				return
			}
		}
	})
	slices.SortStableFunc(jobs, func(a, b declaration.JobView) int {
		left, _ := a.Pos()
		right, _ := b.Pos()
		if left.Line != right.Line {
			return left.Line - right.Line
		}
		return left.Col - right.Col
	})
	input := make([]jobneeds.Job, 0, len(jobs))
	for _, job := range jobs {
		item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID().Value(), Start: reporting.Position(reporting.ViewPosition(job.ID().Pos()))}}
		for dep := range job.Needs() {
			item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value(), Start: reporting.Position(reporting.ViewPosition(dep.Pos()))})
		}
		input = append(input, item)
	}
	return reporting.FromDiagnostics(jobneeds.Check(input))
}
