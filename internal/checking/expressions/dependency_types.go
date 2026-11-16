package expressions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/contexts/outputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Get type of `outputs.<output name>`
func (rule *Checker) getActionOutputsType(spec *definition.String) types.ObjectType {
	if spec == nil {
		return outputs.ActionType(actionmeta.Definition{})
	}

	metadata, err := rule.actionDefinition(spec.Value)
	if err != nil {
		// Action validation reports lookup failures; unknown outputs stay open here.
		rule.debug("Could not infer action outputs: %v", err)
		return outputs.ActionType(actionmeta.Definition{})
	}
	return outputs.ActionType(metadata)
}

func (rule *Checker) workflowCallMetadata(call *definition.WorkflowCall) *calls.Metadata {
	if call.Uses == nil {
		return nil
	}

	m, err := rule.workflowMetadata(call.Uses.Text())
	if err != nil {
		// The workflow-call rule reports metadata failures.
		rule.debug("Could not infer workflow outputs: %v", err)
		return nil
	}
	return m
}

func (rule *Checker) calcNeedsType(job workflows.Job) types.ObjectType {
	resolved := map[workflows.Job]*calls.Metadata{}
	for _, dependency := range outputs.DirectDependencies(rule.workflow, job) {
		if call := dependency.WorkflowCall; call != nil {
			resolved[dependency] = rule.workflowCallMetadata(call)
		}
	}
	return outputs.NeedsType(rule.workflow, job, resolved)
}

func (rule *Checker) checkWorkflowCallOutputs(call *definition.WorkflowCallEvent, workflow workflows.Workflow) {
	hasOutputs := false
	for range call.Outputs {
		hasOutputs = true
		break
	}
	if !hasOutputs {
		return
	}
	resolved := map[workflows.Job]*calls.Metadata{}
	for _, job := range workflow.Jobs() {
		if target := job.WorkflowCall; target != nil {
			resolved[job] = rule.workflowCallMetadata(target)
		}
	}
	jobs := outputs.JobsType(workflow, resolved)
	if jobs.Len() == 0 {
		return
	}
	rule.jobsTy = new(jobs)
	for _, output := range call.Outputs {
		rule.checkString(output.Value, "on.workflow_call.outputs.<output_id>.value")
	}
}
