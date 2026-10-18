package expressions

import (
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	exprlang "actionlint.kjanat.dev/internal/expression"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/references"
)

// Get type of `outputs.<output name>`
func (rule *Checker) getActionOutputsType(spec *workflowlang.String) *exprlang.ObjectType {
	if spec == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	localSpec := spec.Value
	if s, ok := references.SelfRepositoryLocal(localSpec); ok {
		localSpec = s
	}
	if strings.HasPrefix(localSpec, "./") {
		meta, err := rule.actionMetadata(spec.Value)
		if err != nil {
			rule.Error(spec.Pos, err.Error())
			return exprlang.NewMapObjectType(exprlang.StringType{})
		}
		if meta == nil {
			return exprlang.NewMapObjectType(exprlang.StringType{})
		}

		return typeOfActionOutputs(meta)
	}

	// github-script action allows to set any outputs through calling `core.setOutput` directly.
	// So any `outputs.*` properties should be accepted (#104)
	if strings.HasPrefix(spec.Value, "actions/github-script@") {
		return exprlang.NewEmptyObjectType()
	}

	// When the action run at this step is a popular action, we know what outputs are set by it.
	// Set the output names to `steps.{step_id}.outputs.{name}`.
	if meta := rule.popularAction(spec.Value); meta != nil {
		return typeOfActionOutputs(meta)
	}

	return exprlang.NewMapObjectType(exprlang.StringType{})
}

func (rule *Checker) getWorkflowCallOutputsType(call *workflowlang.WorkflowCall) *exprlang.ObjectType {
	if call.Uses == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	m, err := rule.workflowMetadata(call.Uses.Value)
	if err != nil {
		rule.errs = append(rule.errs, findingmodel.At(call.Uses.Pos, "workflow-call", err.Error()))
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}
	if m == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	p := make(map[string]exprlang.Type, len(m.Outputs))
	for n := range m.Outputs {
		p[n] = exprlang.StringType{}
	}
	return exprlang.NewStrictObjectType(p)
}

func (rule *Checker) calcNeedsType(job *workflowlang.Job) *exprlang.ObjectType {
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	o := exprlang.NewEmptyStrictObjectType()
	rule.populateDependantNeedsTypes(o, job, job)
	return o
}

func (rule *Checker) populateDependantNeedsTypes(out *exprlang.ObjectType, job *workflowlang.Job, root *workflowlang.Job) {
	for _, id := range job.Needs {
		i := strings.ToLower(id.Value) // ID is case insensitive
		if i == root.ID.Value {
			continue // When cyclic dependency exists. This does not happen normally.
		}
		if _, ok := out.Props[i]; ok {
			continue // Already added
		}

		j, ok := rule.workflow.Jobs[i]
		if !ok {
			continue
		}

		var outputs *exprlang.ObjectType
		if j.WorkflowCall == nil {
			outputs = exprlang.NewEmptyStrictObjectType()
			for name := range j.Outputs {
				outputs.Props[name] = exprlang.StringType{}
			}
		} else {
			outputs = rule.getWorkflowCallOutputsType(j.WorkflowCall)
		}

		out.Props[i] = exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs": outputs,
			"result":  exprlang.StringType{},
		})

		// Do not collect outputs type from parent of parent recursively. (#151)
	}
}

func (rule *Checker) checkWorkflowCallOutputs(outputs map[string]*workflowlang.WorkflowCallEventOutput, jobs map[string]*workflowlang.Job) {
	if len(outputs) == 0 || len(jobs) == 0 {
		return
	}

	props := make(map[string]exprlang.Type, len(jobs))
	for n, j := range jobs {
		var o *exprlang.ObjectType
		if j.WorkflowCall != nil {
			// Outputs are not defined in jobs.<job_id> section when it is reusable workflow call.
			o = exprlang.NewEmptyObjectType()
		} else {
			p := make(map[string]exprlang.Type, len(j.Outputs))
			for n := range j.Outputs {
				p[n] = exprlang.StringType{}
			}
			o = exprlang.NewStrictObjectType(p)
		}
		props[n] = exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs": o,
			"result":  exprlang.StringType{},
		})
	}
	rule.jobsTy = exprlang.NewStrictObjectType(props)

	for _, o := range outputs {
		rule.checkString(o.Value, "on.workflow_call.outputs.<output_id>.value")
	}
}

func typeOfActionOutputs(meta *actionmeta.Metadata) *exprlang.ObjectType {
	// Some action sets outputs dynamically. Such outputs are not defined in action.yml. actionlint
	// cannot check such outputs statically so it allows any props (#18)
	if meta.SkipOutputs {
		return exprlang.NewEmptyObjectType()
	}
	object := exprlang.NewEmptyStrictObjectType()
	for n := range meta.Outputs {
		object.SetProperty(n, exprlang.StringType{})
	}
	return object
}
