package engine

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/internal/checks/jobneeds"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleJobNeeds adapts complete workflow declarations to the dependency checker.
// It has no state accumulated between job visits.
type RuleJobNeeds struct{ RuleBase }

func NewRuleJobNeeds() *RuleJobNeeds { return &RuleJobNeeds{RuleBase: builtinRuleBase("job-needs")} }
func (rule *RuleJobNeeds) VisitWorkflowPost(workflow *workflowlang.Workflow) error {
	jobs := slices.Collect(maps.Values(workflow.Jobs))
	slices.SortStableFunc(jobs, func(a, b *workflowlang.Job) int {
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line - b.Pos.Line
		}
		return a.Pos.Col - b.Pos.Col
	})
	input := make([]jobneeds.Job, 0, len(jobs))
	for _, job := range jobs {
		item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID.Value, Start: factPosition(job.ID.Pos)}}
		for _, dep := range job.Needs {
			item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value, Start: factPosition(dep.Pos)})
		}
		input = append(input, item)
	}
	for _, finding := range jobneeds.Check(input) {
		rule.errs = append(rule.errs, legacyDiagnosticError(finding))
	}
	return nil
}
