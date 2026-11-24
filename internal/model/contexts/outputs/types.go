// Package outputs derives expression output types from published declarations
// and supplied dependency metadata.
package outputs

import (
	"iter"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func ActionType(definition actionmeta.Definition) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	if !definition.OutputsComplete() {
		object.SetMapped(exprtypes.StringType())
	}
	for _, name := range definition.OutputNames() {
		object.SetProperty(name, exprtypes.StringType())
	}
	return object.Build()
}

// WorkflowType describes a reusable workflow's declared outputs. Nil metadata
// leaves names unknown; non-nil metadata with no outputs proves an empty set.
func WorkflowType(metadata *calls.Metadata) exprtypes.ObjectType {
	if metadata == nil {
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}
	var object exprtypes.ObjectBuilder
	for _, name := range metadata.Outputs.Names() {
		object.SetProperty(name, exprtypes.StringType())
	}
	return object.Build()
}

func StepType(outputs exprtypes.ObjectType) exprtypes.ObjectType {
	return exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"outputs": outputs.Type(), "conclusion": exprtypes.StringType(), "outcome": exprtypes.StringType(),
	})
}

func DirectDependencies(workflow workflows.BoundWorkflow, job workflows.BoundJob) iter.Seq2[string, workflows.BoundJob] {
	return func(yield func(string, workflows.BoundJob) bool) {
		seen := map[string]bool{}
		for _, reference := range job.Needs {
			name := strings.ToLower(reference.Text())
			if name == strings.ToLower(job.ID.Text()) || seen[name] {
				continue
			}
			dependency, exists := workflow.Job(name)
			if !exists {
				continue
			}
			seen[name] = true
			if !yield(name, dependency) {
				return
			}
		}
	}
}

// JobType leaves output names unknown for an absent or nil reusable call entry.
func JobType(job workflows.BoundJob, reusableWorkflows map[workflows.BoundJob]*calls.Metadata) exprtypes.ObjectType {
	var outputs exprtypes.ObjectType
	if job.WorkflowCall != nil {
		outputs = WorkflowType(reusableWorkflows[job])
	} else {
		var declared exprtypes.ObjectBuilder
		for name := range job.Outputs {
			declared.SetProperty(name, exprtypes.StringType())
		}
		outputs = declared.Build()
	}
	return exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"outputs": outputs.Type(), "result": exprtypes.StringType(),
	})
}

// NeedsType includes direct prerequisites only and never reads reusable workflow files.
func NeedsType(workflow workflows.BoundWorkflow, job workflows.BoundJob, reusableWorkflows map[workflows.BoundJob]*calls.Metadata) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	for name, dependency := range DirectDependencies(workflow, job) {
		object.Set(name, JobType(dependency, reusableWorkflows).Type())
	}
	return object.Build()
}

func JobsType(workflow workflows.BoundWorkflow, reusableWorkflows map[workflows.BoundJob]*calls.Metadata) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	for name, job := range workflow.Jobs() {
		object.Set(name, JobType(job, reusableWorkflows).Type())
	}
	return object.Build()
}
