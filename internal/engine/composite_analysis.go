package engine

import (
	"context"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// compositeAnalyzer owns script-analyzer policy and invocation-specific state,
// leaving Visitor responsible for the workflow's ordinary pass lifecycle.
type compositeAnalyzer struct {
	resolveScriptExecution            bool
	scripts                           []scriptUnit
	scriptScopes                      []*scriptScope
	scriptScope                       *scriptScope
	scriptPaths                       execution.Paths
	scriptJobShell, scriptRunnerShell execution.ShellValue
	scriptWorkflowDir, scriptJobDir   execution.Directory
	scriptConfig                      *Config
	collectScripts                    bool
	workflowScriptShell               *string
	jobScriptShell                    *string
	ctx                               context.Context
	actions                           *actionlocal.Resolver
	passes                            []workflowvisit.Pass
	rules                             []compositeScriptRules
	workflowEnv                       *workflowlang.Env
	workflowShell                     execution.ShellValue
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
	analysis.scriptWorkflowDir = execution.DefaultDirectory(workflow.Defaults)
	analysis.workflowScriptShell = scriptDefaultShell(workflow.Defaults)
	analysis.workflowEnv = workflow.Env
	analysis.workflowShell = execution.DefaultShell(workflow.Defaults)
}

func (analysis *compositeAnalyzer) beginJob(job *workflowlang.Job) {
	analysis.jobScriptShell = scriptDefaultShell(job.Defaults)
	analysis.scriptJobShell = execution.DefaultShell(job.Defaults)
	analysis.scriptJobDir = execution.DefaultDirectory(job.Defaults)
	analysis.scriptRunnerShell = execution.ShellValue{}
	if runnerlang.PlatformOf(job.RunsOn) == runnerlang.Windows {
		analysis.scriptRunnerShell = execution.ShellValueOf(&workflowlang.String{Value: "pwsh"})
	}
	if container := execution.ContainerShell(job.Container); container.Kind != execution.ShellUnspecified {
		analysis.scriptRunnerShell = container
	}
	analysis.jobID = ""
	if job.ID != nil {
		analysis.jobID = job.ID.Value
	}
	analysis.actions.BeginJob(analysis.workflowEnv, analysis.workflowShell, job)
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.InvalidateCheckout()
}

func (analysis *compositeAnalyzer) visitStep(step *workflowlang.Step) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	analysis.actions.ObserveExecution(step)
	analysis.actions.ObserveCheckout(step)
	analysis.collectScript(step, analysis.scriptConfig, "", nil, analysis.jobScriptShell, analysis.workflowScriptShell)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if participant, ok := pass.(invocationParticipant); ok && participant.deferActionInvocation() {
			if _, action := step.Exec.(*workflowlang.ExecAction); action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := visitWithIgnoreOrigin(pass, findingmodel.StepOrigin(analysis.jobID, step), hasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	if _, action := step.Exec.(*workflowlang.ExecAction); !action {
		return nil
	}
	var scripts []workflowvisit.Rule
	for _, pass := range analysis.passes {
		if participant, ok := pass.(compositeParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	return analysis.visitActionScripts(step, scripts, make(map[string]bool))
}
