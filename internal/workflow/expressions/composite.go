package expressions

import (
	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// CheckCompositeScript checks script sinks independently of workflow caller types.
// Metadata validation owns context/name errors, so do not duplicate those findings.
func (rule *Checker) CheckCompositeScript(step *workflowlang.Step) {
	run, ok := step.Exec.(*workflowlang.ExecRun)
	if !ok {
		return
	}
	if run == nil || run.Run == nil {
		return
	}
	previous := rule.workflow
	previousComposite := rule.compositeScript
	rule.compositeScript = true
	rule.workflow = &workflowlang.Workflow{ExpressionSources: map[workflowlang.Pos]*workflowlang.ScriptSource{*run.Run.Pos: run.Source}}
	defer func() { rule.workflow, rule.compositeScript = previous, previousComposite }()
	// Caller inputs and previous step outputs have no supplied value/type snapshot.
	rule.inputsTy = exprlang.NewMapObjectType(exprlang.StringType{})
	rule.stepsTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.matrixTy = exprlang.NewMapObjectType(exprlang.AnyType{})
	rule.scriptEnv = step.Env
	defer func() { rule.scriptEnv = nil }()
	rule.checkScriptString(run.Run, "jobs.<job_id>.steps.run", rule.scriptShell(run))
}
