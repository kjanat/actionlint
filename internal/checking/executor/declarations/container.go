package declarations

import (
	"context"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/input/source"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type containerFrame struct {
	context invocation.Context
	guards  int
}

type containerAdapter struct {
	workflowvisit.Base
	ctx        context.Context
	runners    *runnerlang.Knowledge
	actions    *workspaceactions.PreparedActions
	remote     *actionremote.ResolvedActions
	targets    []runnerlang.ContainerTarget
	guards     []string
	frames     []containerFrame
	conditions map[source.Reference]string
}

// NewContainerRunner supplies container facts to the rule and tracks enclosing
// composite conditions. The composition coordinator acquires action metadata.
func NewContainerRunner(ctx context.Context, runners *runnerlang.Knowledge, actions *workspaceactions.Resolver, remote *actionremote.ResolvedActions) workflowvisit.Rule {
	descriptor := correctness.ContainerRunnerDeclaration()
	return &containerAdapter{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), ctx: ctx, runners: runners, actions: actions.Prepared(), remote: remote, conditions: map[source.Reference]string{}}
}

func (r *containerAdapter) VisitJobPre(job workflowdecl.JobView) error {
	r.targets = r.runners.ContainerLimitations(job)
	r.guards = []string{job.If().Value()}
	r.reportUses(r.runners.JobContainerUses(job))
	return nil
}

func (r *containerAdapter) reportUses(uses []runnerlang.ContainerUse) {
	for _, finding := range correctness.ContainerRunnerFindings(uses) {
		r.Report(findingmodel.FromDiagnostic(finding.Diagnostic))
	}
}

func (r *containerAdapter) VisitStep(step workflowdecl.StepView) error {
	r.conditions[step.Reference()] = step.If().Value()
	return r.checkStep(step)
}

func (r *containerAdapter) checkStep(step workflowdecl.StepView) error {
	if len(r.targets) == 0 {
		return nil
	}
	action, ok := step.Action()
	if !ok || !action.Uses().Present() || action.Uses().ContainsExpression() {
		return nil
	}
	spec := action.Uses().Value()
	docker := strings.HasPrefix(spec, "docker://")
	if !docker && (strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/")) {
		result, _ := r.actions.LookupMetadata(spec)
		docker = result.Status() == workspaceactions.MetadataDockerfileOnly
		if meta := result.Metadata(); meta != nil {
			docker = strings.EqualFold(meta.Runs.Using, "docker")
		}
	} else if !docker {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		result := r.remote.Inspect(spec)
		docker = strings.EqualFold(result.Definition().Using(), "docker")
	}
	if docker {
		r.reportUses(runnerlang.DockerActionUses(step, r.targets, r.guards))
	}
	return nil
}

func (r *containerAdapter) ApplyCheckout(actions *workspaceactions.Resolver) {
	r.actions = actions.Prepared()
}

func (r *containerAdapter) EnterInvocation(scope checkcalls.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.frames = append(r.frames, containerFrame{context: scope.Context, guards: len(r.guards)})
	guard := r.conditions[scope.Context.Site()]
	if scope.Context.Condition() == invocation.ConditionSkipped {
		guard = "false"
	}
	r.guards = append(r.guards, guard)
	if body, available := scope.Source.ActionView(); available {
		for step := range body.Steps() {
			r.conditions[step.Reference()] = step.If().Value()
		}
	}
	return nil
}

func (r *containerAdapter) LeaveInvocation(scope invocation.Context) error {
	if len(r.frames) == 0 || r.frames[len(r.frames)-1].context != scope {
		return invocation.ErrForeignContext
	}
	frame := r.frames[len(r.frames)-1]
	r.guards = r.guards[:frame.guards]
	r.frames = r.frames[:len(r.frames)-1]
	return nil
}

func (r *containerAdapter) VisitInvocationStep(workflowdecl.StepView) error { return nil }

func (r *containerAdapter) VisitOpaqueAction(step workflowdecl.StepView) error {
	if len(r.frames) == 0 {
		return nil
	}
	return r.checkStep(step)
}
