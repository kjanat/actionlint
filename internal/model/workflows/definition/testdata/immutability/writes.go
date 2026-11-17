// Package immutability holds write patterns for TestPublishedSyntaxWritesFixture.
package immutability

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Rejected writes published syntax reached through pointers, slices and maps.
func Rejected(step *workflowlang.Step, workflow *workflowlang.Workflow, job *workflowlang.Job) {
	step.Name = nil
	step.ID.Value = "changed"
	job.Steps = append(job.Steps, step)
	workflow.Jobs["added"] = job
	delete(workflow.Jobs, "added")
	step.Pos.Line++
}

// Allowed writes a local copy and builds a fresh node.
func Allowed(pos *source.Position) *workflowlang.Step {
	local := *pos
	local.Column++
	return &workflowlang.Step{Pos: &local}
}
