package expressions

import (
	"iter"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// CheckCompositeScript checks script sinks independently of workflow caller types.
// Metadata validation owns context/name errors, so do not duplicate those findings.
func (rule *Checker) CheckCompositeScript(step workflowdecl.StepView) {
	previous := *rule
	// This pass owns script findings only; metadata validation owns every other
	// composite diagnostic, including shape errors reported outside exprError.
	rule.errs = nil
	defer func() {
		for _, r := range rule.errs {
			if r.rule == "script-injection" || r.rule == "script-interpolation" {
				previous.errs = append(previous.errs, r)
			}
		}
		*rule = previous
	}()
	rule.compositeScript = true
	rule.workflow = workflowdecl.WorkflowView{}
	rule.scriptSources = map[workflowlang.Pos]workflowdecl.ScriptSourceView{}
	// Caller inputs and previous step outputs have no supplied value/type snapshot.
	rule.inputsTy = exprlang.NewMapObjectType(exprlang.StringType{})
	rule.stepsTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.matrixTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.matrixScriptSafe = nil
	rule.jobEnv, rule.scriptJob = workflowdecl.EnvView{}, workflowdecl.JobView{}
	rule.scriptEnv = step.Env()
	if run, ok := step.Run(); ok {
		if !run.Run().Present() {
			return
		}
		if position, present := run.Run().Pos(); present {
			rule.scriptSources[position] = run.Source()
		}
		rule.checkScriptString(run.Run(), "jobs.<job_id>.steps.run", rule.scriptShell(step))
	} else if action, ok := step.Action(); ok {
		var collect func(iter.Seq2[string, workflowdecl.InputView], iter.Seq[workflowdecl.ActionInputMappingView])
		collect = func(inputs iter.Seq2[string, workflowdecl.InputView], insertions iter.Seq[workflowdecl.ActionInputMappingView]) {
			for _, input := range inputs {
				if position, present := input.Value().Pos(); present {
					rule.scriptSources[position] = input.Source()
				}
			}
			for insertion := range insertions {
				collect(insertion.Inputs(), insertion.Insertions())
			}
		}
		collect(action.Inputs(), action.InputInsertions())
		rule.checkActionInputs(action, action.Uses())
	}
}
