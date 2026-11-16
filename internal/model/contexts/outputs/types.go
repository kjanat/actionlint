// Package outputs derives expression output types from published declarations
// and supplied dependency metadata. It performs no lookups or rule checks.
package outputs

import (
	"iter"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ActionType distinguishes an exhaustive output declaration from unknown names.
// Output values are strings even when their names could not be determined.
func ActionType(definition actionmeta.Definition) types.ObjectType {
	var object types.ObjectBuilder
	if !definition.OutputsComplete() {
		object.SetMapped(types.StringType())
	}
	for _, name := range definition.OutputIDs() {
		object.SetProperty(name, types.StringType())
	}
	return object.Build()
}

// WorkflowType describes a reusable workflow's declared outputs. Nil metadata
// leaves names unknown; non-nil metadata with no outputs proves an empty set.
func WorkflowType(metadata *calls.Metadata) types.ObjectType {
	if metadata == nil {
		return types.NewMapObjectType(types.StringType())
	}
	var object types.ObjectBuilder
	for name := range metadata.Outputs {
		object.SetProperty(name, types.StringType())
	}
	return object.Build()
}

// StepType adds the string-valued conclusion and outcome to supplied output
// types. Use ActionType for action metadata or unknown run-step output names.
func StepType(outputs types.ObjectType) types.ObjectType {
	return types.NewStrictObjectType(map[string]types.Type{
		"outputs": outputs.Type(), "conclusion": types.StringType(), "outcome": types.StringType(),
	})
}

// DirectDependencies yields existing direct prerequisites once each. Missing
// jobs, duplicate references and self references do not introduce context entries.
// Each iteration has independent state.
func DirectDependencies(workflow workflows.Workflow, job workflows.Job) iter.Seq2[string, workflows.Job] {
	return func(yield func(string, workflows.Job) bool) {
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

// JobType supplies outputs and result for either the jobs or needs context.
// reusableWorkflows contains already loaded metadata keyed by the declaring job.
// An absent or nil entry for a reusable call leaves output names unknown.
func JobType(job workflows.Job, reusableWorkflows map[workflows.Job]*calls.Metadata) types.ObjectType {
	var outputs types.ObjectType
	if job.WorkflowCall != nil {
		outputs = WorkflowType(reusableWorkflows[job])
	} else {
		var declared types.ObjectBuilder
		for name := range job.Outputs {
			declared.SetProperty(name, types.StringType())
		}
		outputs = declared.Build()
	}
	return types.NewStrictObjectType(map[string]types.Type{
		"outputs": outputs.Type(), "result": types.StringType(),
	})
}

// NeedsType includes direct prerequisites only. The caller supplies reusable
// workflow metadata; this function never resolves or reads their source files.
func NeedsType(workflow workflows.Workflow, job workflows.Job, reusableWorkflows map[workflows.Job]*calls.Metadata) types.ObjectType {
	var object types.ObjectBuilder
	for name, dependency := range DirectDependencies(workflow, job) {
		object.Set(name, JobType(dependency, reusableWorkflows).Type())
	}
	return object.Build()
}

// JobsType exposes the same job output shapes when a reusable workflow maps its
// job outputs to on.workflow_call.outputs. Context availability is checked elsewhere.
func JobsType(workflow workflows.Workflow, reusableWorkflows map[workflows.Job]*calls.Metadata) types.ObjectType {
	var object types.ObjectBuilder
	for name, job := range workflow.Jobs() {
		object.Set(name, JobType(job, reusableWorkflows).Type())
	}
	return object.Build()
}
