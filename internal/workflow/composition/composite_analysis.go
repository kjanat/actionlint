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
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// compositionSession owns the graph and accumulated results shared by invocations.
type compositionSession struct {
	scriptInputs           []analyzer.ScriptInput
	scriptInputsForConfig  func(*repoconfig.Config) ([]analyzer.ScriptInput, error)
	scriptCatalogues       map[*repoconfig.Config][]analyzer.ScriptInput
	runners                *runnerlang.Knowledge
	resolveScriptExecution bool
	scripts                []scriptexecution.ScriptRequest
	scriptScopes           []*scriptexecution.Scope
	scriptPaths            execution.Paths
	reader                 *fsread.Reader
	collectScripts         bool
	ctx                    context.Context
	passes                 []workflowvisit.Pass
	metadataFindings       []*findingmodel.Finding
	configForFile          func(string) (*repoconfig.Config, error)
	sourceSnapshots        *documentparse.SourceSnapshots
	invocations            *invocation.Builder
	budget                 invocation.Budget
	interrupted            bool
}

// compositeAnalyzer is one traversal frame. Child calls share session results,
// but cannot replace their caller's source, configuration or invocation context.
type compositeAnalyzer struct {
	*compositionSession
	actions                           *actionlocal.Resolver
	sourceSnapshot                    documentparse.SourceSnapshot
	scriptScope                       *scriptexecution.Scope
	scriptConfig                      *repoconfig.Config
	context                           invocation.Context
	scriptJobShell, scriptRunnerShell value.Value
	scriptWorkflowDir, scriptJobDir   execution.Directory
	workflowScriptShell               *string
	jobScriptShell                    *string
	workflowEnv                       mapping.Mapping
	workflowShell                     value.Value
	jobID                             string
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(declaration workflowdecl.WorkflowView) error {
	if err := analysis.sourceSnapshot.ValidateWorkflow(declaration); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("workflow source snapshot is unavailable")
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
	if analysis.sourceSnapshot.Kind() != documentparse.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	validated, err := scriptexecution.NewDeployment(analysis.sourceSnapshots, analysis.sourceSnapshot, deployment.Address(), deployment.SourcePath(), deployment.ActionPath() == "")
	if err != nil || validated != deployment {
		return fmt.Errorf("%w: action deployment disagrees with source snapshot", scriptexecution.ErrInvalidBatch)
	}
	analysis.beginEntryScope(deployment)
	return nil
}

func (analysis *compositeAnalyzer) beginEntryScope(deployment scriptexecution.Deployment) {
	analysis.scriptScope = &scriptexecution.Scope{
		Paths: analysis.scriptPaths, Reader: analysis.reader, Config: analysis.scriptConfig,
		Deployment: deployment, Context: analysis.context,
	}
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, deployment.ActionPath())
	analysis.scriptScopes = append(analysis.scriptScopes, analysis.scriptScope)
}

func (analysis *compositeAnalyzer) beginJob(declaration workflowdecl.JobView) error {
	if err := analysis.sourceSnapshot.ValidateJob(declaration); err != nil {
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

func (analysis *compositeAnalyzer) visitStep(step workflowdecl.StepView) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	if err := analysis.cancelled(); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("step source snapshot is unavailable")
	}
	declaration := step
	analysis.actions.ObserveExecution(declaration)
	analysis.actions.ObserveCheckout(declaration)
	if err := analysis.collectScript(step, analysis.scriptConfig, analysis.jobScriptShell, analysis.workflowScriptShell); err != nil {
		return err
	}
	if _, action := declaration.Action(); !action {
		return analysis.visitStepPasses(step)
	}
	var scripts []builtin.InvocationParticipant
	for _, pass := range analysis.passes {
		if participant, ok := pass.(builtin.InvocationParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	return analysis.visitActionScripts(step, scripts, func() error { return analysis.visitStepPasses(step) })
}

func (analysis *compositeAnalyzer) visitStepPasses(step workflowdecl.StepView) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("step source snapshot is unavailable")
	}
	declaration := step
	origin := canonicalStepOrigin(declaration, analysis.jobID)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if err := checks.VisitWithOrigin(pass, origin, checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	return nil
}

// visitJobPass attributes what a pass reports while visiting a job to that job.
func (analysis *compositeAnalyzer) visitJobPass(pass workflowvisit.Pass, job workflowdecl.JobView, visit func() error) error {
	return checks.VisitWithOrigin(pass, canonicalOrigin(job.Origin()), checks.HasConfiguredIgnores(analysis.scriptConfig), visit)
}

func canonicalStepOrigin(step workflowdecl.StepView, job string) *findingmodel.IgnoreOrigin {
	return canonicalOrigin(step.Origin(job))
}

func canonicalOrigin(owned workflowdecl.Origin) *findingmodel.IgnoreOrigin {
	origin := &findingmodel.IgnoreOrigin{Job: owned.Job, StepID: owned.StepID, StepName: owned.StepName, Uses: owned.Uses}
	if owned.HasPosition {
		position := owned.Position
		origin.StepPosition = &position
	}
	return origin
}
