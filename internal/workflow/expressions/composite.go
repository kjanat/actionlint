package expressions

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// CheckCompositeScript checks script sinks independently of workflow caller types.
// Metadata validation owns context/name errors, so do not duplicate those findings.
func (rule *Checker) CheckCompositeScript(step *workflowlang.Step) {
	previous := *rule
	// This pass owns script findings only; metadata validation owns every other
	// composite diagnostic, including shape errors reported outside exprError.
	rule.errs = nil
	defer func() {
		for _, finding := range rule.errs {
			if finding.Rule == "script-injection" || finding.Rule == "script-interpolation" {
				previous.errs = append(previous.errs, finding)
			}
		}
		*rule = previous
	}()
	rule.compositeScript = true
	rule.workflow = &workflowlang.Workflow{ExpressionSources: map[workflowlang.Pos]*workflowlang.ScriptSource{}}
	// Caller inputs and previous step outputs have no supplied value/type snapshot.
	rule.inputsTy = exprlang.NewMapObjectType(exprlang.StringType{})
	rule.stepsTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.matrixTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.matrixScriptSafe = nil
	rule.jobEnv, rule.scriptJob = nil, nil
	rule.scriptEnv = step.Env
	switch exec := step.Exec.(type) {
	case *workflowlang.ExecRun:
		if exec == nil || exec.Run == nil {
			return
		}
		rule.workflow.ExpressionSources[*exec.Run.Pos] = exec.Source
		rule.checkScriptString(exec.Run, "jobs.<job_id>.steps.run", rule.scriptShell(exec))
	case *workflowlang.ExecAction:
		if exec == nil {
			return
		}
		var collect func(*workflowlang.ActionInputMapping)
		collect = func(mapping *workflowlang.ActionInputMapping) {
			for _, input := range mapping.Inputs {
				if input.Value != nil && input.Value.Pos != nil {
					rule.workflow.ExpressionSources[*input.Value.Pos] = input.Source
				}
			}
			for _, insertion := range mapping.Insertions {
				collect(insertion)
			}
		}
		collect(&workflowlang.ActionInputMapping{Inputs: exec.Inputs, Insertions: exec.InputInsertions})
		rule.checkActionInputs(workflowlang.InputMapping(exec), exec.Uses)
	}
}
