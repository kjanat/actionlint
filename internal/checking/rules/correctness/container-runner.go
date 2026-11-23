package correctness

import (
	"context"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var ContainerRunnerRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "container-runner",
	Description:      "Report Docker container requirements for selected runners",
	Category:         "correctness",
	Subcategory:      "runners",
	Recommended:      true,
	Explanation:      "Job containers, service containers and Docker actions need Linux and a Docker daemon.",
	Help:             "Use a Linux runner with Docker, such as ubuntu-latest, or skip the container step on incompatible matrix rows. On ubuntu-slim, suppress the warning only if a remote Docker daemon is configured.",
	Examples: []rule.Example{{
		Before: "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: windows-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
		After:  "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
	}},
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	Walker: func(c define.RuleInputs, base workflowvisit.Base) workflowvisit.Rule {
		return newContainerRunner(c.Context, base, c.Runners, c.Actions, c.RemoteActions)
	},
}

func ContainerRunnerFindings(uses []runners.ContainerUse) []reporting.StepFinding {
	findings := make([]reporting.StepFinding, 0, len(uses))
	for _, use := range uses {
		var description string
		switch use.Kind {
		case runners.JobContainer:
			description = "job container"
		case runners.ServiceContainer:
			description = "service " + use.Name
		case runners.DockerAction:
			description = fmt.Sprintf("Docker action %q", use.Name)
		}
		var message string
		var level severity.Severity
		switch use.Target.Limitation {
		case runners.ContainerRequiresLinux:
			message = fmt.Sprintf("%s requires a Linux runner with Docker. Runner selection %q resolves to a non-Linux runner", description, use.Target.Runner)
			level = severity.Error
		case runners.ContainerMissingDaemon:
			message = fmt.Sprintf("%s requires a Docker daemon. Runner selection %q has Docker client tools but cannot run containers locally. Configure a compatible remote daemon or select a Docker-capable Linux runner", description, use.Target.Runner)
			level = severity.Warning
		case runners.ContainerUnknown:
			continue
		}
		findings = append(findings, reporting.StepFinding{Message: message, Severity: level, Start: use.Position})
	}
	return findings
}

type containerFrame struct {
	context invocation.Context
	guards  int
}

type containerAdapter struct {
	workflowvisit.Base
	ctx        context.Context
	runners    *runners.Knowledge
	actions    *localactions.PreparedActions
	remote     *remote.ResolvedActions
	targets    []runners.ContainerTarget
	guards     []string
	frames     []containerFrame
	conditions map[sourcetext.Reference]string
}

func newContainerRunner(ctx context.Context, base workflowvisit.Base, runners *runners.Knowledge, actions *localactions.Resolver, remote *remote.ResolvedActions) workflowvisit.Rule {
	return &containerAdapter{Base: base, ctx: ctx, runners: runners, actions: actions.Prepared(), remote: remote, conditions: map[sourcetext.Reference]string{}}
}

func (r *containerAdapter) VisitJobPre(job workflows.BoundJob) error {
	r.targets = r.runners.ContainerLimitations(job)
	r.guards = []string{job.If.Text()}
	r.reportUses(r.runners.JobContainerUses(job))
	return nil
}

func (r *containerAdapter) reportUses(uses []runners.ContainerUse) {
	for _, finding := range ContainerRunnerFindings(uses) {
		r.Report(new(finding.Finding))
	}
}

func (r *containerAdapter) VisitStep(step workflows.BoundStep) error {
	r.conditions[step.Reference()] = step.If.Text()
	return r.checkStep(step)
}

func (r *containerAdapter) checkStep(step workflows.BoundStep) error {
	if len(r.targets) == 0 {
		return nil
	}
	action, ok := step.Action()
	if !ok || action.Uses == nil || action.Uses.ContainsExpression() {
		return nil
	}
	spec := action.Uses.Text()
	form := callreferences.FormOf(spec)
	docker := form == callreferences.FormContainer
	if !docker && (form == callreferences.FormWorkspace || form == callreferences.FormSelfRepository) {
		result, _ := r.actions.LookupMetadata(spec)
		docker = result.Status() == localactions.MetadataDockerfileOnly
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
		r.reportUses(runners.DockerActionUses(step, r.targets, r.guards))
	}
	return nil
}

func (r *containerAdapter) ApplyCheckout(actions *localactions.Resolver) {
	r.actions = actions.Prepared()
}

func (r *containerAdapter) EnterInvocation(scope workflowvisit.Invocation) error {
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
			r.conditions[step.Reference()] = step.If.Text()
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

func (r *containerAdapter) VisitInvocationStep(workflows.BoundStep) error { return nil }

func (r *containerAdapter) VisitOpaqueAction(step workflows.BoundStep) error {
	if len(r.frames) == 0 {
		return nil
	}
	return r.checkStep(step)
}
