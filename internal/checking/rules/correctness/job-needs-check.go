package correctness

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/jobneeds"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkJobDependencies(w declaration.Workflow) []reporting.Finding {
	jobs := slices.Collect(func(yield func(declaration.Job) bool) {
		for _, job := range w.Jobs() {
			if !yield(job) {
				return
			}
		}
	})
	slices.SortStableFunc(jobs, func(a, b declaration.Job) int {
		left, _ := a.Position()
		right, _ := b.Position()
		if left.Line != right.Line {
			return left.Line - right.Line
		}
		return left.Column - right.Column
	})
	input := make([]jobneeds.Job, 0, len(jobs))
	for _, job := range jobs {
		item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID.Text(), Start: reporting.Position(reporting.OptionalPosition(job.ID.Position()))}}
		for _, dep := range job.Needs {
			item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value, Start: reporting.Position(dep.Pos)})
		}
		input = append(input, item)
	}
	return reporting.FromDiagnostics(jobneeds.Check(input))
}
