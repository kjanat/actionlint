// Package outputs derives expression output types from published declarations
// and supplied dependency metadata. It performs no lookups or rule checks.
package outputs

import (
	"iter"
	"strings"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ActionType distinguishes an exhaustive output declaration from unknown names.
// Output values are strings even when their names could not be determined.
func ActionType(definition actionmeta.Definition) exprtypes.ObjectType {
	if !definition.Present() || definition.SkipOutputs() {
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}
	var object exprtypes.ObjectBuilder
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
	for name := range metadata.Outputs {
		object.SetProperty(name, exprtypes.StringType())
	}
	return object.Build()
}

// StepType adds the string-valued conclusion and outcome to supplied output
// types. Use ActionType for action metadata or unknown run-step output names.
func StepType(outputs exprtypes.ObjectType) exprtypes.ObjectType {
	return exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"outputs": outputs.Type(), "conclusion": exprtypes.StringType(), "outcome": exprtypes.StringType(),
	})
}

// DirectDependencies yields existing direct prerequisites once each. Missing
// jobs, duplicate references and self references do not introduce context entries.
// Each iteration has independent state.
func DirectDependencies(workflow declaration.WorkflowView, job declaration.JobView) iter.Seq2[string, declaration.JobView] {
	return func(yield func(string, declaration.JobView) bool) {
		seen := map[string]bool{}
		for reference := range job.Needs() {
			name := strings.ToLower(reference.Value())
			if name == strings.ToLower(job.ID().Value()) || seen[name] {
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
func JobType(job declaration.JobView, reusableWorkflows map[declaration.JobView]*calls.Metadata) exprtypes.ObjectType {
	var outputs exprtypes.ObjectType
	if job.WorkflowCall().Present() {
		outputs = WorkflowType(reusableWorkflows[job])
	} else {
		var declared exprtypes.ObjectBuilder
		for name := range job.Outputs() {
			declared.SetProperty(name, exprtypes.StringType())
		}
		outputs = declared.Build()
	}
	return exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"outputs": outputs.Type(), "result": exprtypes.StringType(),
	})
}

// NeedsType includes direct prerequisites only. The caller supplies reusable
// workflow metadata; this function never resolves or reads their source files.
func NeedsType(workflow declaration.WorkflowView, job declaration.JobView, reusableWorkflows map[declaration.JobView]*calls.Metadata) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	for name, dependency := range DirectDependencies(workflow, job) {
		object.Set(name, JobType(dependency, reusableWorkflows).Type())
	}
	return object.Build()
}

// JobsType exposes the same job output shapes when a reusable workflow maps its
// job outputs to on.workflow_call.outputs. Context availability is checked elsewhere.
func JobsType(workflow declaration.WorkflowView, reusableWorkflows map[declaration.JobView]*calls.Metadata) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	for name, job := range workflow.Jobs() {
		object.Set(name, JobType(job, reusableWorkflows).Type())
	}
	return object.Build()
}
