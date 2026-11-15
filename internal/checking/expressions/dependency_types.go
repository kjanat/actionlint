package expressions

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	contextoutputs "actionlint.kjanat.dev/v2/internal/model/contexts/outputs"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Get type of `outputs.<output name>`
func (rule *Checker) getActionOutputsType(spec *workflowlang.String) exprtypes.ObjectType {
	if spec == nil {
		return contextoutputs.ActionType(actionmeta.Definition{})
	}

	definition, err := rule.actionDefinition(spec.Value)
	if err != nil {
		// Action validation reports lookup failures; unknown outputs stay open here.
		rule.debug("Could not infer action outputs: %v", err)
		return contextoutputs.ActionType(actionmeta.Definition{})
	}
	return contextoutputs.ActionType(definition)
}

func (rule *Checker) workflowCallMetadata(call *workflowlang.WorkflowCall) *calls.Metadata {
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

func (rule *Checker) calcNeedsType(job workflowdecl.Job) exprtypes.ObjectType {
	resolved := map[workflowdecl.Job]*calls.Metadata{}
	for _, dependency := range contextoutputs.DirectDependencies(rule.workflow, job) {
		if call := dependency.WorkflowCall; call != nil {
			resolved[dependency] = rule.workflowCallMetadata(call)
		}
	}
	return contextoutputs.NeedsType(rule.workflow, job, resolved)
}

func (rule *Checker) checkWorkflowCallOutputs(call *workflowlang.WorkflowCallEvent, workflow workflowdecl.Workflow) {
	hasOutputs := false
	for range call.Outputs {
		hasOutputs = true
		break
	}
	if !hasOutputs {
		return
	}
	resolved := map[workflowdecl.Job]*calls.Metadata{}
	for _, job := range workflow.Jobs() {
		if target := job.WorkflowCall; target != nil {
			resolved[job] = rule.workflowCallMetadata(target)
		}
	}
	jobs := contextoutputs.JobsType(workflow, resolved)
	if jobs.Len() == 0 {
		return
	}
	rule.jobsTy = new(jobs)
	for _, output := range call.Outputs {
		rule.checkString(output.Value, "on.workflow_call.outputs.<output_id>.value")
	}
}
