package expressions

import (
	"strings"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Get type of `outputs.<output name>`
func (rule *Checker) getActionOutputsType(spec workflowdecl.StringView) exprtypes.ObjectType {
	if !spec.Present() {
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}

	definition, err := rule.actionDefinition(spec.Value())
	if err != nil {
		// Action validation reports lookup failures; unknown outputs stay open here.
		rule.debug("Could not infer action outputs: %v", err)
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}
	if definition.Present() {
		if definition.SkipOutputs() {
			return exprtypes.NewEmptyObjectType()
		}
		var object exprtypes.ObjectBuilder
		for _, name := range definition.OutputNames() {
			object.SetProperty(name, exprtypes.StringType())
		}
		return object.Build()
	}

	return exprtypes.NewMapObjectType(exprtypes.StringType())
}

func (rule *Checker) getWorkflowCallOutputsType(call workflowdecl.WorkflowCallView) exprtypes.ObjectType {
	if !call.Uses().Present() {
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}

	m, err := rule.workflowMetadata(call.Uses().Value())
	if err != nil {
		// The workflow-call rule reports metadata failures.
		rule.debug("Could not infer workflow outputs: %v", err)
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}
	if m == nil {
		return exprtypes.NewMapObjectType(exprtypes.StringType())
	}

	p := make(map[string]exprtypes.Type, len(m.Outputs))
	for n := range m.Outputs {
		p[n] = exprtypes.StringType()
	}
	return exprtypes.NewStrictObjectType(p)
}

func (rule *Checker) calcNeedsType(job workflowdecl.JobView) exprtypes.ObjectType {
	var object exprtypes.ObjectBuilder
	for id := range job.Needs() {
		name := strings.ToLower(id.Value())
		if name == job.ID().Value() {
			continue
		}
		if object.Has(name) {
			continue
		}
		dependency, exists := rule.workflow.Job(name)
		if !exists {
			continue
		}
		var outputs exprtypes.ObjectType
		if call := dependency.WorkflowCall(); call.Present() {
			outputs = rule.getWorkflowCallOutputsType(call)
		} else {
			var declared exprtypes.ObjectBuilder
			for name := range dependency.Outputs() {
				declared.Set(name, exprtypes.StringType())
			}
			outputs = declared.Build()
		}
		object.Set(name, exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"outputs": outputs.Type(), "result": exprtypes.StringType()}).Type())
		// Needs contains direct dependencies, not grandparents (#151).
	}
	return object.Build()
}

func (rule *Checker) checkWorkflowCallOutputs(call workflowdecl.WorkflowCallEventView, workflow workflowdecl.WorkflowView) {
	hasOutputs := false
	for range call.Outputs() {
		hasOutputs = true
		break
	}
	if !hasOutputs {
		return
	}
	properties := map[string]exprtypes.Type{}
	for name, job := range workflow.Jobs() {
		var outputs exprtypes.ObjectType
		if job.WorkflowCall().Present() {
			outputs = exprtypes.NewEmptyObjectType()
		} else {
			values := map[string]exprtypes.Type{}
			for name := range job.Outputs() {
				values[name] = exprtypes.StringType()
			}
			outputs = exprtypes.NewStrictObjectType(values)
		}
		properties[name] = exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"outputs": outputs.Type(), "result": exprtypes.StringType()}).Type()
	}
	if len(properties) == 0 {
		return
	}
	rule.jobsTy = new(exprtypes.NewStrictObjectType(properties))
	for _, output := range call.Outputs() {
		rule.checkString(output.Value(), "on.workflow_call.outputs.<output_id>.value")
	}
}
