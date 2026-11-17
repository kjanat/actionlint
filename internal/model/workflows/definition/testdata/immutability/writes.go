// Package immutability holds write patterns for TestPublishedSyntaxWritesFixture.
package immutability

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Rejected writes published syntax reached through pointers, slices and maps.
func Rejected(step *definition.Step, workflow *definition.Workflow, job *definition.Job) {
	step.Name = nil
	step.ID.Value = "changed"
	job.Steps = append(job.Steps, step)
	workflow.Jobs["added"] = job
	delete(workflow.Jobs, "added")
	step.Pos.Line++
}

// Allowed writes a local copy and builds a fresh node.
func Allowed(pos *sourcetext.Position) *definition.Step {
	local := *pos
	local.Column++
	return &definition.Step{Pos: &local}
}
