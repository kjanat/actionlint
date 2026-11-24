package composites

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"

	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"

	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"

	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type compositionSession struct {
	bindSource             func(*rulebinding.Scope, string) (*rulebinding.Scope, error)
	scriptInputs           []checkinputs.ScriptInput
	scriptInputsForScope   func(*rulebinding.Scope) ([]checkinputs.ScriptInput, error)
	scriptCatalogues       map[*rulebinding.Scope][]checkinputs.ScriptInput
	runners                *runners.Knowledge
	resolveScriptExecution bool
	scripts                []scriptanalysis.ScriptRequest
	scriptScopes           []*scriptanalysis.Scope
	scriptPaths            execution.Paths
	reader                 *files.Reader
	collectScripts         bool
	ctx                    context.Context
	passes                 []workflowvisit.Pass
	metadataFindings       []*finding.Finding
	metadataCopies         *finding.Copier
	incompleteChecks       []report.IncompleteCheck

	configProvenance configuration.Provenance
	sourceSnapshots  *parsing.SourceSnapshots
	captures         *capture.Store
	remoteActions    *remote.Service
	remoteDocuments  map[string]remoteDocument
	onRemoteDocument func(capture.CapturedDocument, parsing.Document)
	remoteLookups    map[actions.Reference]struct{}
	prepareSource    func(parsing.SourceSnapshot, []string) error
	invocations      *invocation.Builder
	budget           invocation.Budget
	interrupted      bool
}

type compositeAnalyzer struct {
	*compositionSession
	bindings                          *rulebinding.Scope
	actions                           *localactions.Resolver
	sourceSnapshot                    parsing.SourceSnapshot
	scriptScope                       *scriptanalysis.Scope
	scriptConfig                      *configuration.Config
	context                           invocation.Context
	scriptJobShell, scriptRunnerShell value.Value
	scriptWorkflowDir, scriptJobDir   execution.Directory
	workflowScriptShell               *string
	jobScriptShell                    *string
	workflowEnv                       mappings.Mapping
	workflowShell                     value.Value
	jobID                             string
	environmentLayers                 []scripts.EnvironmentLayer
	operatingSystems                  []runnerapi.OS
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(declaration workflows.BoundWorkflow) error {
	if err := analysis.sourceSnapshot.ValidateWorkflow(declaration); err != nil {
		return err
	}
	analysis.beginEntryScope(scriptanalysis.Deployment{})
	defaults := workflows.DefaultsRun(declaration.Defaults)
	analysis.scriptWorkflowDir = execution.DefaultDirectory(defaults)
	analysis.workflowScriptShell = scriptDefaultShell(declaration.Defaults)
	analysis.workflowEnv = workflows.Environment(declaration.Env)
	analysis.environmentLayers = []scripts.EnvironmentLayer{scripts.DeclaredEnvironment(scripts.EnvironmentWorkflow, analysis.workflowEnv)}
	analysis.workflowShell = execution.DefaultShell(defaults)
	return nil
}

func (analysis *compositeAnalyzer) beginAction(deployment scriptanalysis.Deployment) error {
	if analysis.sourceSnapshot.Kind() != parsing.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	validated, err := scriptanalysis.NewDeployment(analysis.sourceSnapshots, analysis.sourceSnapshot, deployment.Address(), deployment.SourcePath(), deployment.ActionPath() == "")
	if err != nil || validated != deployment {
		return fmt.Errorf("%w: action deployment disagrees with source snapshot", scriptanalysis.ErrInvalidBatch)
	}
	analysis.beginEntryScope(deployment)
	analysis.environmentLayers = []scripts.EnvironmentLayer{{Scope: scripts.EnvironmentCaller, UnknownNames: true}}
	return nil
}

func (analysis *compositeAnalyzer) beginEntryScope(deployment scriptanalysis.Deployment) {
	analysis.scriptScope = &scriptanalysis.Scope{
		Bindings: analysis.bindings,
		Paths:    analysis.scriptPaths, Reader: analysis.reader, Config: analysis.scriptConfig, ConfigProvenance: analysis.configProvenance,
		Deployment: deployment, Context: analysis.context,
	}
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, deployment.ActionPath())
	analysis.scriptScopes = append(analysis.scriptScopes, analysis.scriptScope)
}

func (analysis *compositeAnalyzer) beginJob(declaration workflows.BoundJob) error {
	if err := analysis.sourceSnapshot.ValidateJob(declaration); err != nil {
		return err
	}
	analysis.jobScriptShell = scriptDefaultShell(declaration.Defaults)
	defaults := workflows.DefaultsRun(declaration.Defaults)
	analysis.scriptJobShell = execution.DefaultShell(defaults)
	analysis.scriptJobDir = execution.DefaultDirectory(defaults)
	knowledge := analysis.runners.Job(declaration)
	analysis.operatingSystems = knowledge.OperatingSystems()
	if container := scripts.ContainerShell(workflows.Container(declaration.Container)); container.State() != value.Missing {
		analysis.operatingSystems = []runnerapi.OS{runnerapi.UnknownOS}
		if container.Known() {
			analysis.operatingSystems = []runnerapi.OS{runnerapi.Linux}
		}
	}
	analysis.environmentLayers = []scripts.EnvironmentLayer{
		scripts.DeclaredEnvironment(scripts.EnvironmentWorkflow, analysis.workflowEnv),
		scripts.DeclaredEnvironment(scripts.EnvironmentJob, workflows.Environment(declaration.Env)),
	}
	analysis.scriptRunnerShell = scripts.DefaultShell(workflows.Container(declaration.Container), knowledge.Shell)
	analysis.jobID = ""
	if id := declaration.ID; id != nil {
		analysis.jobID = id.Value
	}
	analysis.actions.BeginJob(analysis.workflowEnv, analysis.workflowShell, declaration)
	return nil
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.InvalidateCheckout()
}

func (analysis *compositeAnalyzer) visitStep(step workflows.BoundStep) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	if err := analysis.cancelled(); err != nil {
		return err
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
	var scriptParticipants []workflowvisit.InvocationParticipant
	for _, pass := range analysis.passes {
		if participant, ok := pass.(workflowvisit.InvocationParticipant); ok {
			scriptParticipants = append(scriptParticipants, participant)
		}
	}
	return analysis.visitActionScripts(step, scriptParticipants, func() error { return analysis.visitStepPasses(step) })
}

func (analysis *compositeAnalyzer) visitStepPasses(step workflows.BoundStep) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	declaration := step
	origin := declaration.IgnoreOrigin(analysis.jobID)
	for _, pass := range analysis.passes {
		compositeCheckoutPaths(pass, analysis.actions)
		if err := workflowvisit.VisitWithOrigin(pass, origin, workflowvisit.HasConfiguredIgnores(analysis.scriptConfig), func() error { return pass.VisitStep(step) }); err != nil {
			return err
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) visitJobPass(pass workflowvisit.Pass, job workflows.BoundJob, visit func() error) error {
	return workflowvisit.VisitWithOrigin(pass, job.IgnoreOrigin(), workflowvisit.HasConfiguredIgnores(analysis.scriptConfig), visit)
}
