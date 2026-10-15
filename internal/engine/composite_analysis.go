package engine

import (
	"context"
	"errors"
	"maps"
	"slices"
)

// compositeAnalyzer owns script-analyzer policy and invocation-specific state,
// leaving Visitor responsible for the workflow's ordinary pass lifecycle.
type compositeAnalyzer struct {
	ctx              context.Context
	actions          *LocalActionsCache
	passes           []Pass
	rules            []compositeScriptRules
	workflowStart    int
	actionPathErrors map[string]error
	workflowEnv      *Env
	workflowShell    shellValue
	configForFile    func(string) (*Config, error)
	jobID            string
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(workflow *Workflow) {
	analysis.workflowStart = len(analysis.rules)
	analysis.actionPathErrors = map[string]error{}
	analysis.workflowEnv = workflow.Env
	analysis.workflowShell = defaultsShellValue(workflow.Defaults)
}

func (analysis *compositeAnalyzer) deferWorkflowError(pass Pass, err error) bool {
	if errors.Is(err, errConfigActionPathUnavailable) {
		if owner, ok := pass.(interface{ Name() string }); ok {
			analysis.actionPathErrors[owner.Name()] = err
			return true
		}
	}
	return false
}

func (analysis *compositeAnalyzer) validateWorkflow() error {
	for _, composite := range analysis.rules[analysis.workflowStart:] {
		for _, rule := range composite.rules {
			delete(analysis.actionPathErrors, rule.Name())
		}
	}
	failures := make([]error, 0, len(analysis.actionPathErrors))
	for _, name := range slices.Sorted(maps.Keys(analysis.actionPathErrors)) {
		failures = append(failures, analysis.actionPathErrors[name])
	}
	return errors.Join(failures...)
}

func (analysis *compositeAnalyzer) beginJob(job *Job) {
	analysis.jobID = ""
	if job.ID != nil {
		analysis.jobID = job.ID.Value
	}
	analysis.actions.setCheckout(runDirectory{})
	analysis.actions.platform = runnerPlatform(job.RunsOn)
	analysis.actions.caseInsensitive = analysis.actions.platform == platformKindWindows || macOSRunner(job.RunsOn)
	analysis.actions.checkoutEnvUnknown = checkoutEnvironmentUnknown(analysis.workflowEnv, analysis.actions.platform) || checkoutEnvironmentUnknown(job.Env, analysis.actions.platform)
	analysis.actions.persistentEnvUnknown = false
	analysis.actions.shellEnvUnknown = analysis.actions.shellEnvironmentUnknown(analysis.workflowEnv) || analysis.actions.shellEnvironmentUnknown(job.Env)
	runnerShell := shellValue{kind: shellValueUnknown}
	switch analysis.actions.platform {
	case platformKindWindows:
		runnerShell = shellValueFromString(&String{Value: "pwsh"})
	case platformKindMacOrLinux:
		runnerShell = shellValue{}
	}
	if container := shellcheckContainerShell(job.Container); container.kind != shellValueUnspecified {
		runnerShell = container
	}
	analysis.actions.checkoutShell = resolveRunShell(&ExecRun{}, defaultsShellValue(job.Defaults), analysis.workflowShell, runnerShell)
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.setCheckout(runDirectory{kind: directoryUnknown})
}

func (analysis *compositeAnalyzer) visitStep(step *Step) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	analysis.actions.observeExecution(step)
	analysis.actions.observeCheckout(step)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if participant, ok := pass.(invocationParticipant); ok && participant.deferActionInvocation() {
			if _, action := step.Exec.(*ExecAction); action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := visitWithIgnoreOrigin(pass, newDiagnosticIgnoreOrigin(analysis.jobID, step), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	if _, action := step.Exec.(*ExecAction); !action {
		return nil
	}
	var scripts []Rule
	for _, pass := range analysis.passes {
		if participant, ok := pass.(compositeParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	return analysis.visitActionScripts(step, scripts, make(map[string]bool))
}
