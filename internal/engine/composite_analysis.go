package engine

import (
	"context"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// compositeAnalyzer owns script-analyzer policy and invocation-specific state,
// leaving Visitor responsible for the workflow's ordinary pass lifecycle.
type compositeAnalyzer struct {
	resolveScriptExecution            bool
	scripts                           []scriptUnit
	scriptScopes                      []*scriptScope
	scriptScope                       *scriptScope
	scriptPaths                       runPaths
	scriptJobShell, scriptRunnerShell shellValue
	scriptWorkflowDir, scriptJobDir   runDirectory
	scriptConfig                      *Config
	collectScripts                    bool
	workflowScriptShell               *string
	jobScriptShell                    *string
	ctx                               context.Context
	actions                           *LocalActionsCache
	passes                            []Pass
	rules                             []compositeScriptRules
	workflowEnv                       *workflowlang.Env
	workflowShell                     shellValue
	configForFile                     func(string) (*Config, error)
	jobID                             string
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(workflow *workflowlang.Workflow) {
	analysis.scriptScope = &scriptScope{paths: analysis.scriptPaths, config: analysis.scriptConfig}
	analysis.scriptScopes = append(analysis.scriptScopes, analysis.scriptScope)
	analysis.scriptWorkflowDir = defaultsWorkingDirectory(workflow.Defaults)
	analysis.workflowScriptShell = scriptDefaultShell(workflow.Defaults)
	analysis.workflowEnv = workflow.Env
	analysis.workflowShell = defaultsShellValue(workflow.Defaults)
}

func (analysis *compositeAnalyzer) beginJob(job *workflowlang.Job) {
	analysis.jobScriptShell = scriptDefaultShell(job.Defaults)
	analysis.scriptJobShell = defaultsShellValue(job.Defaults)
	analysis.scriptJobDir = defaultsWorkingDirectory(job.Defaults)
	analysis.scriptRunnerShell = shellValue{}
	if runnerPlatform(job.RunsOn) == platformKindWindows {
		analysis.scriptRunnerShell = shellValueFromString(&workflowlang.String{Value: "pwsh"})
	}
	if container := shellcheckContainerShell(job.Container); container.kind != shellValueUnspecified {
		analysis.scriptRunnerShell = container
	}
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
		runnerShell = shellValueFromString(&workflowlang.String{Value: "pwsh"})
	case platformKindMacOrLinux:
		runnerShell = shellValue{}
	}
	if container := shellcheckContainerShell(job.Container); container.kind != shellValueUnspecified {
		runnerShell = container
	}
	analysis.actions.checkoutShell = resolveRunShell(&workflowlang.ExecRun{}, defaultsShellValue(job.Defaults), analysis.workflowShell, runnerShell)
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.setCheckout(runDirectory{kind: directoryUnknown})
}

func (analysis *compositeAnalyzer) visitStep(step *workflowlang.Step) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	analysis.actions.observeExecution(step)
	analysis.actions.observeCheckout(step)
	analysis.collectScript(step, analysis.scriptConfig, "", nil, analysis.jobScriptShell, analysis.workflowScriptShell)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if participant, ok := pass.(invocationParticipant); ok && participant.deferActionInvocation() {
			if _, action := step.Exec.(*workflowlang.ExecAction); action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := visitWithIgnoreOrigin(pass, newDiagnosticIgnoreOrigin(analysis.jobID, step), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	if _, action := step.Exec.(*workflowlang.ExecAction); !action {
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
