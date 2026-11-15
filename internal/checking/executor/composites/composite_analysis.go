package composites

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/selection"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"

	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// compositionSession owns the graph and accumulated results shared by invocations.
type compositionSession struct {
	bindSource             func(*binding.Scope, string) (*binding.Scope, error)
	scriptInputs           []checkinputs.ScriptInput
	scriptInputsForScope   func(*binding.Scope) ([]checkinputs.ScriptInput, error)
	scriptCatalogues       map[*binding.Scope][]checkinputs.ScriptInput
	runners                *runnerlang.Knowledge
	resolveScriptExecution bool
	scripts                []scriptexecution.ScriptRequest
	scriptScopes           []*scriptexecution.Scope
	scriptPaths            execution.Paths
	reader                 *files.Reader
	collectScripts         bool
	ctx                    context.Context
	passes                 []workflowvisit.Pass
	metadataFindings       []*findingmodel.Finding
	metadataCopies         *findingmodel.Copier
	incompleteChecks       []report.IncompleteCheck

	configProvenance repoconfig.Provenance
	sourceSnapshots  *parsing.SourceSnapshots
	captures         *capture.Store
	remoteActions    *remote.Service
	remoteDocuments  map[string]remoteDocument
	remoteLookups    map[actions.Reference]struct{}
	prepareSource    func(parsing.SourceSnapshot, []string) error
	invocations      *invocation.Builder
	budget           invocation.Budget
	interrupted      bool
}

// compositeAnalyzer is one traversal frame. Child calls share session results,
// but cannot replace their caller's source, configuration or invocation context.
type compositeAnalyzer struct {
	*compositionSession
	bindings                          *binding.Scope
	actions                           *workspaceactions.Resolver
	sourceSnapshot                    parsing.SourceSnapshot
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
	environmentLayers                 []scriptmodel.EnvironmentLayer
	operatingSystems                  []runnerapi.OS
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(declaration workflowdecl.Workflow) error {
	if err := analysis.sourceSnapshot.ValidateWorkflow(declaration); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("workflow source snapshot is unavailable")
	}
	analysis.beginEntryScope(scriptexecution.Deployment{})
	defaults := workflowdecl.DefaultsRun(declaration.Defaults)
	analysis.scriptWorkflowDir = execution.DefaultDirectory(defaults)
	analysis.workflowScriptShell = scriptDefaultShell(declaration.Defaults)
	analysis.workflowEnv = workflowdecl.Environment(declaration.Env)
	analysis.environmentLayers = []scriptmodel.EnvironmentLayer{scriptmodel.DeclaredEnvironment(scriptmodel.EnvironmentWorkflow, analysis.workflowEnv)}
	analysis.workflowShell = execution.DefaultShell(defaults)
	return nil
}

func (analysis *compositeAnalyzer) beginAction(deployment scriptexecution.Deployment) error {
	if analysis.sourceSnapshot.Kind() != parsing.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	validated, err := scriptexecution.NewDeployment(analysis.sourceSnapshots, analysis.sourceSnapshot, deployment.Address(), deployment.SourcePath(), deployment.ActionPath() == "")
	if err != nil || validated != deployment {
		return fmt.Errorf("%w: action deployment disagrees with source snapshot", scriptexecution.ErrInvalidBatch)
	}
	analysis.beginEntryScope(deployment)
	analysis.environmentLayers = []scriptmodel.EnvironmentLayer{{Scope: scriptmodel.EnvironmentCaller, UnknownNames: true}}
	return nil
}

func (analysis *compositeAnalyzer) beginEntryScope(deployment scriptexecution.Deployment) {
	analysis.scriptScope = &scriptexecution.Scope{
		Bindings: analysis.bindings,
		Paths:    analysis.scriptPaths, Reader: analysis.reader, Config: analysis.scriptConfig, ConfigProvenance: analysis.configProvenance,
		Deployment: deployment, Context: analysis.context,
	}
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, deployment.ActionPath())
	analysis.scriptScopes = append(analysis.scriptScopes, analysis.scriptScope)
}

func (analysis *compositeAnalyzer) beginJob(declaration workflowdecl.Job) error {
	if err := analysis.sourceSnapshot.ValidateJob(declaration); err != nil {
		return err
	}
	analysis.jobScriptShell = scriptDefaultShell(declaration.Defaults)
	defaults := workflowdecl.DefaultsRun(declaration.Defaults)
	analysis.scriptJobShell = execution.DefaultShell(defaults)
	analysis.scriptJobDir = execution.DefaultDirectory(defaults)
	knowledge := analysis.runners.Job(declaration)
	analysis.operatingSystems = knowledge.OperatingSystems()
	if container := scriptmodel.ContainerShell(workflowdecl.Container(declaration.Container)); container.State() != value.Missing {
		analysis.operatingSystems = []runnerapi.OS{runnerapi.UnknownOS}
		if container.Known() {
			analysis.operatingSystems = []runnerapi.OS{runnerapi.Linux}
		}
	}
	analysis.environmentLayers = []scriptmodel.EnvironmentLayer{
		scriptmodel.DeclaredEnvironment(scriptmodel.EnvironmentWorkflow, analysis.workflowEnv),
		scriptmodel.DeclaredEnvironment(scriptmodel.EnvironmentJob, workflowdecl.Environment(declaration.Env)),
	}
	analysis.scriptRunnerShell = scriptmodel.DefaultShell(workflowdecl.Container(declaration.Container), knowledge.Shell)
	analysis.jobID = ""
	if id := declaration.ID; id != nil {
		analysis.jobID = id.Value()
	}
	analysis.actions.BeginJob(analysis.workflowEnv, analysis.workflowShell, declaration)
	return nil
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.InvalidateCheckout()
}

func (analysis *compositeAnalyzer) visitStep(step workflowdecl.Step) error {
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
	if err := analysis.collectScript(step, analysis.jobScriptShell, analysis.workflowScriptShell); err != nil {
		return err
	}
	if _, action := declaration.Action(); !action {
		return analysis.visitStepPasses(step)
	}
	var scripts []checkcalls.InvocationParticipant
	for _, pass := range analysis.passes {
		if participant, ok := pass.(checkcalls.InvocationParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	return analysis.visitActionScripts(step, scripts, func() error { return analysis.visitStepPasses(step) })
}

func (analysis *compositeAnalyzer) visitStepPasses(step workflowdecl.Step) error {
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
		if err := selection.VisitWithOrigin(pass, origin, selection.HasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	return nil
}

// visitJobPass attributes what a pass reports while visiting a job to that job.
func (analysis *compositeAnalyzer) visitJobPass(pass workflowvisit.Pass, job workflowdecl.Job, visit func() error) error {
	return selection.VisitWithOrigin(pass, canonicalOrigin(job.Origin()), selection.HasConfiguredIgnores(analysis.scriptConfig), visit)
}

func canonicalStepOrigin(step workflowdecl.Step, job string) *findingmodel.IgnoreOrigin {
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
