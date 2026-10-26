package composition

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"

	checks "actionlint.kjanat.dev/v2/internal/checks"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
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
	scripts                           []scriptexecution.ScriptRequest
	scriptScopes                      []*scriptexecution.Scope
	scriptScope                       *scriptexecution.Scope
	scriptPaths                       execution.Paths
	scriptJobShell, scriptRunnerShell value.Value
	scriptWorkflowDir, scriptJobDir   execution.Directory
	scriptConfig                      *repoconfig.Config
	collectScripts                    bool
	workflowScriptShell               *string
	jobScriptShell                    *string
	ctx                               context.Context
	actions                           *actionlocal.Resolver
	passes                            []workflowvisit.Pass
	rules                             []compositeScriptRules
	workflowEnv                       mapping.Mapping
	workflowShell                     value.Value
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

func (analysis *compositeAnalyzer) beginWorkflow(workflow *workflowlang.Workflow) error {
	if analysis.sourceSnapshot == nil {
		return errors.New("workflow source snapshot is unavailable")
	}
	declaration, present := analysis.sourceSnapshot.WorkflowView()
	if !present {
		return errors.New("workflow declaration is unavailable")
	}
	analysis.beginEntryScope(scriptexecution.Deployment{})
	defaults := declaration.Defaults().Run().Semantic()
	analysis.scriptWorkflowDir = execution.DefaultDirectory(defaults)
	analysis.workflowScriptShell = scriptDefaultShell(declaration.Defaults())
	analysis.workflowEnv = declaration.Env().Mapping()
	analysis.workflowShell = execution.DefaultShell(defaults)
	return nil
}

func (analysis *compositeAnalyzer) beginAction(deployment scriptexecution.Deployment) error {
	if analysis.sourceSnapshot == nil || analysis.sourceSnapshot.Kind() != documentparse.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	validated, err := scriptexecution.NewDeployment(analysis.sourceSnapshot, deployment.SourcePath(), deployment.ActionPath() == "")
	if err != nil || validated != deployment {
		return fmt.Errorf("%w: action deployment disagrees with source snapshot", scriptexecution.ErrInvalidBatch)
	}
	analysis.beginEntryScope(deployment)
	return nil
}

func (analysis *compositeAnalyzer) beginEntryScope(deployment scriptexecution.Deployment) {
	analysis.scriptScope = &scriptexecution.Scope{
		Paths: analysis.scriptPaths, Config: analysis.scriptConfig,
		Deployment: deployment, Context: analysis.context,
	}
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, deployment.ActionPath())
	analysis.scriptScopes = append(analysis.scriptScopes, analysis.scriptScope)
}

func (analysis *compositeAnalyzer) beginJob(job *workflowlang.Job) error {
	declaration, err := analysis.sourceSnapshot.JobView(job)
	if err != nil {
		return err
	}
	analysis.jobScriptShell = scriptDefaultShell(declaration.Defaults())
	defaults := declaration.Defaults().Run().Semantic()
	analysis.scriptJobShell = execution.DefaultShell(defaults)
	analysis.scriptJobDir = execution.DefaultDirectory(defaults)
	knowledge := analysis.runners.Job(declaration)
	analysis.scriptRunnerShell = value.DynamicValue("", value.KindUnknown, value.Origin{})
	if analysis.runners == nil {
		analysis.scriptRunnerShell = value.YAMLScalar("bash", workflowlang.YamlTagStr, value.Origin{})
	}
	if knowledge.Shell != "" {
		analysis.scriptRunnerShell = value.YAMLScalar(knowledge.Shell, workflowlang.YamlTagStr, value.Origin{})
	}
	if container := execution.ContainerShell(declaration.Container().Semantic()); container.State() != value.Missing {
		analysis.scriptRunnerShell = container
	}
	analysis.jobID = ""
	if id := declaration.ID(); id.Present() {
		analysis.jobID = id.Value()
	}
	analysis.actions.BeginJob(analysis.workflowEnv, analysis.workflowShell, declaration)
	return nil
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.InvalidateCheckout()
}

func (analysis *compositeAnalyzer) visitStep(step *workflowlang.Step) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	if analysis.sourceSnapshot == nil {
		return errors.New("step source snapshot is unavailable")
	}
	declaration, err := analysis.sourceSnapshot.StepView(step)
	if err != nil {
		return err
	}
	analysis.actions.ObserveExecution(declaration)
	analysis.actions.ObserveCheckout(declaration)
	if err := analysis.collectScript(step, analysis.scriptConfig, analysis.jobScriptShell, analysis.workflowScriptShell); err != nil {
		return err
	}
	if _, action := declaration.Action(); !action {
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
	if analysis.sourceSnapshot == nil {
		return errors.New("step source snapshot is unavailable")
	}
	declaration, err := analysis.sourceSnapshot.StepView(step)
	if err != nil {
		return err
	}
	_, action := declaration.Action()
	origin := canonicalStepOrigin(declaration, analysis.jobID)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if participant, ok := pass.(builtin.InvocationParticipant); ok && participant.DeferActionInvocation() {
			if action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := checks.VisitWithOrigin(pass, origin, checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	return nil
}

func canonicalStepOrigin(step documentparse.StepView, job string) *findingmodel.IgnoreOrigin {
	owned := step.Origin(job)
	origin := &findingmodel.IgnoreOrigin{Job: owned.Job, StepID: owned.StepID, StepName: owned.StepName, Uses: owned.Uses}
	if owned.HasPosition {
		position := owned.Position
		origin.StepPosition = &position
	}
	return origin
}
