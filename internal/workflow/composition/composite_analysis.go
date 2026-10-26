package composition

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"

	checks "actionlint.kjanat.dev/v2/internal/checks"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// compositeAnalyzer owns script-analyzer policy and invocation-specific state,
// leaving Visitor responsible for the workflow's ordinary pass lifecycle.
type compositeAnalyzer struct {
	scriptInputs                      []analyzer.ScriptInput
	scriptInputsForConfig             func(*repoconfig.Config) ([]analyzer.ScriptInput, error)
	scriptCatalogues                  map[*repoconfig.Config][]analyzer.ScriptInput
	runners                           *runnerlang.Knowledge
	resolveScriptExecution            bool
	scripts                           []scriptexecution.Unit
	scriptScopes                      []*scriptexecution.Scope
	scriptScope                       *scriptexecution.Scope
	scriptPaths                       execution.Paths
	scriptJobShell, scriptRunnerShell execution.ShellValue
	scriptWorkflowDir, scriptJobDir   execution.Directory
	scriptConfig                      *repoconfig.Config
	collectScripts                    bool
	workflowScriptShell               *string
	jobScriptShell                    *string
	ctx                               context.Context
	actions                           *actionlocal.Resolver
	passes                            []workflowvisit.Pass
	rules                             []compositeScriptRules
	workflowEnv                       *workflowlang.Env
	workflowShell                     execution.ShellValue
	configForFile                     func(string) (*repoconfig.Config, error)
	jobID                             string
	sourceSnapshots                   *documentparse.SourceSnapshots
	sourceSnapshot                    *documentparse.SourceSnapshot
	invocations                       *invocation.Builder
	context                           invocation.Context
	budget                            invocation.Budget
	interrupted                       bool
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(workflow *workflowlang.Workflow) {
	analysis.scriptScope = &scriptexecution.Scope{Paths: analysis.scriptPaths, Config: analysis.scriptConfig, Context: analysis.context}
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, "")
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
	knowledge := analysis.runners.Job(job)
	analysis.scriptRunnerShell = execution.ShellValue{Kind: execution.ShellUnknown}
	if analysis.runners == nil {
		analysis.scriptRunnerShell = execution.ShellValueOf(&workflowlang.String{Value: "bash"})
	}
	if knowledge.Shell != "" {
		analysis.scriptRunnerShell = execution.ShellValueOf(&workflowlang.String{Value: knowledge.Shell})
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
	if err := analysis.collectScript(step, analysis.scriptConfig, "", nil, analysis.jobScriptShell, analysis.workflowScriptShell); err != nil {
		return err
	}
	if _, action := step.Exec.(*workflowlang.ExecAction); !action {
		return analysis.visitStepPasses(step)
	}
	var scripts []workflowvisit.Rule
	for _, pass := range analysis.passes {
		if participant, ok := pass.(builtin.CompositeParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	return analysis.visitActionScripts(step, scripts, func() error { return analysis.visitStepPasses(step) })
}

func (analysis *compositeAnalyzer) visitStepPasses(step *workflowlang.Step) error {
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if participant, ok := pass.(builtin.InvocationParticipant); ok && participant.DeferActionInvocation() {
			if _, action := step.Exec.(*workflowlang.ExecAction); action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := checks.VisitWithOrigin(pass, findingmodel.StepOrigin(analysis.jobID, step), checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	return nil
}
