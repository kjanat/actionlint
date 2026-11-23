package correctness

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/jobneeds"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var JobNeedsRule = &define.Rule{
	Name:        "job-needs",
	Description: "Report undefined job dependencies and dependency cycles",
	Category:    "correctness",
	Subcategory: "jobs",
	Recommended: true,
	Help:        "Reference an existing job ID and remove dependency cycles.",
	Examples: []rule.Example{rule.WorkflowExample("Remove the missing dependency.",
		"    needs: missing\n", "")},
	Checks: define.Fixed(define.Checks{Workflow: checkJobDependencies}),
}

func checkJobDependencies(w workflows.BoundWorkflow) []reporting.StepFinding {
	jobs := slices.Collect(func(yield func(workflows.BoundJob) bool) {
		for _, job := range w.Jobs() {
			if !yield(job) {
				return
			}
		}
	})
	slices.SortStableFunc(jobs, func(a, b workflows.BoundJob) int {
		left, _ := a.Position()
		right, _ := b.Position()
		if left.Line != right.Line {
			return left.Line - right.Line
		}
		return left.Column - right.Column
	})
	input := make([]jobneeds.JobDependencies, 0, len(jobs))
	for _, job := range jobs {
		item := jobneeds.JobDependencies{ID: jobneeds.Name{Value: job.ID.Text(), Start: sourcetext.ZeroIfNil(sourcetext.NilUnlessPresent(job.ID.Position()))}}
		for _, dep := range job.Needs {
			item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value, Start: sourcetext.ZeroIfNil(dep.Pos)})
		}
		input = append(input, item)
	}
	return reporting.FromDiagnostics(jobneeds.Check(input))
}
