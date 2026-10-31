package expressions

import (
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// Get type of `outputs.<output name>`
func (rule *Checker) getActionOutputsType(spec workflowdecl.StringView) exprlang.ObjectType {
	if !spec.Present() {
		return exprlang.NewMapObjectType(exprlang.StringType())
	}

	localSpec := spec.Value()
	if s, ok := references.SelfRepositoryLocal(localSpec); ok {
		localSpec = s
	}
	if strings.HasPrefix(localSpec, "./") {
		meta, err := rule.actionMetadata(spec.Value())
		if err != nil {
			// The action rule reports metadata failures.
			rule.debug("Could not infer action outputs: %v", err)
			return exprlang.NewMapObjectType(exprlang.StringType())
		}
		if meta == nil {
			return exprlang.NewMapObjectType(exprlang.StringType())
		}

		return typeOfActionOutputs(meta)
	}

	if meta, err := rule.remoteAction(spec.Value()); err == nil && meta.Present() {
		if meta.SkipOutputs() {
			return exprlang.NewEmptyObjectType()
		}
		var object exprlang.ObjectBuilder
		for _, name := range meta.OutputNames() {
			object.SetProperty(name, exprlang.StringType())
		}
		return object.Build()
	}

	return exprlang.NewMapObjectType(exprlang.StringType())
}

func (rule *Checker) getWorkflowCallOutputsType(call workflowdecl.WorkflowCallView) exprlang.ObjectType {
	if !call.Uses().Present() {
		return exprlang.NewMapObjectType(exprlang.StringType())
	}

	m, err := rule.workflowMetadata(call.Uses().Value())
	if err != nil {
		// The workflow-call rule reports metadata failures.
		rule.debug("Could not infer workflow outputs: %v", err)
		return exprlang.NewMapObjectType(exprlang.StringType())
	}
	if m == nil {
		return exprlang.NewMapObjectType(exprlang.StringType())
	}

	p := make(map[string]exprlang.Type, len(m.Outputs))
	for n := range m.Outputs {
		p[n] = exprlang.StringType()
	}
	return exprlang.NewStrictObjectType(p)
}

func (rule *Checker) calcNeedsType(job workflowdecl.JobView) exprlang.ObjectType {
	var object exprlang.ObjectBuilder
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
		var outputs exprlang.ObjectType
		if call := dependency.WorkflowCall(); call.Present() {
			outputs = rule.getWorkflowCallOutputsType(call)
		} else {
			var declared exprlang.ObjectBuilder
			for name := range dependency.Outputs() {
				declared.Set(name, exprlang.StringType())
			}
			outputs = declared.Build()
		}
		object.Set(name, exprlang.NewStrictObjectType(map[string]exprlang.Type{"outputs": outputs.Type(), "result": exprlang.StringType()}).Type())
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
	properties := map[string]exprlang.Type{}
	for name, job := range workflow.Jobs() {
		var outputs exprlang.ObjectType
		if job.WorkflowCall().Present() {
			outputs = exprlang.NewEmptyObjectType()
		} else {
			values := map[string]exprlang.Type{}
			for name := range job.Outputs() {
				values[name] = exprlang.StringType()
			}
			outputs = exprlang.NewStrictObjectType(values)
		}
		properties[name] = exprlang.NewStrictObjectType(map[string]exprlang.Type{"outputs": outputs.Type(), "result": exprlang.StringType()}).Type()
	}
	if len(properties) == 0 {
		return
	}
	rule.jobsTy = new(exprlang.NewStrictObjectType(properties))
	for _, output := range call.Outputs() {
		rule.checkString(output.Value(), "on.workflow_call.outputs.<output_id>.value")
	}
}

func typeOfActionOutputs(meta *actionmeta.Metadata) exprlang.ObjectType {
	// Some action sets outputs dynamically. Such outputs are not defined in action.yml. actionlint
	// cannot check such outputs statically so it allows any props (#18)
	if meta.SkipOutputs {
		return exprlang.NewEmptyObjectType()
	}
	var object exprlang.ObjectBuilder
	for n := range meta.Outputs {
		object.SetProperty(n, exprlang.StringType())
	}
	return object.Build()
}
