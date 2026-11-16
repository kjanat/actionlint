package correctness

import (
	"context"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ContainerRunnerRule is the single definition of the container-runner rule.
var ContainerRunnerRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "container-runner",
	Description:      "Report Docker container requirements for selected runners",
	Category:         "correctness",
	Subcategory:      "runners",
	Recommended:      true,
	Explanation:      "GitHub-managed job containers, service containers and Docker actions require Linux and a Docker daemon. Report an error for known Windows or macOS selections. Warn for GitHub-hosted ubuntu-slim: it includes Docker client tools but no local daemon; a compatible remote daemon may satisfy the requirement. Inspect available action metadata and expanded composites, with matrix rows and conditions determining which selections can reach the container. Unknown runner facts and unavailable action bodies do not establish a violation. Docker commands in scripts can use remote daemons and are not classified by this rule.",
	Help:             "Select a Linux runner with Docker, such as ubuntu-latest, or guard the container action so incompatible matrix rows do not execute it. For ubuntu-slim, verify any remote daemon configuration before suppressing the warning. Self-hosted Linux runners need an accessible Docker daemon. No automatic fix is offered because changing the runner can change job behavior.",
	Examples: []rule.Example{{
		Description: "Use a Linux runner with Docker for a job container.",
		Before:      "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: windows-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
		After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
	}},
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	Walker: func(c define.Inputs, base workflowvisit.Base) workflowvisit.Rule {
		return newContainerRunner(c.Context, base, c.Runners, c.Actions, c.RemoteActions)
	},
}

// ContainerRunnerFindings reports established container limitations. Source
// acquisition, condition evaluation and invocation state belong to the host.
func ContainerRunnerFindings(uses []runnerlang.ContainerUse) []reporting.Finding {
	findings := make([]reporting.Finding, 0, len(uses))
	for _, use := range uses {
		var description string
		switch use.Kind {
		case runnerlang.JobContainer:
			description = "job container"
		case runnerlang.ServiceContainer:
			description = "service " + use.Name
		case runnerlang.DockerAction:
			description = fmt.Sprintf("Docker action %q", use.Name)
		}
		var message, severity string
		switch use.Target.Limitation {
		case runnerlang.ContainerRequiresLinux:
			message = fmt.Sprintf("%s requires a Linux runner with Docker. Runner selection %q resolves to a non-Linux runner", description, use.Target.Runner)
			severity = "error"
		case runnerlang.ContainerMissingDaemon:
			message = fmt.Sprintf("%s requires a Docker daemon. Runner selection %q has Docker client tools but cannot run containers locally. Configure a compatible remote daemon or select a Docker-capable Linux runner", description, use.Target.Runner)
			severity = "warning"
		case runnerlang.ContainerUnknown:
			continue
		}
		findings = append(findings, reporting.Finding{Message: message, Severity: severity, Start: use.Position})
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
	runners    *runnerlang.Knowledge
	actions    *workspaceactions.PreparedActions
	remote     *actionremote.ResolvedActions
	targets    []runnerlang.ContainerTarget
	guards     []string
	frames     []containerFrame
	conditions map[source.Reference]string
}

// newContainerRunner supplies container facts to the rule and tracks enclosing
// composite conditions. The composition coordinator acquires action metadata.
func newContainerRunner(ctx context.Context, base workflowvisit.Base, runners *runnerlang.Knowledge, actions *workspaceactions.Resolver, remote *actionremote.ResolvedActions) workflowvisit.Rule {
	return &containerAdapter{Base: base, ctx: ctx, runners: runners, actions: actions.Prepared(), remote: remote, conditions: map[source.Reference]string{}}
}

func (r *containerAdapter) VisitJobPre(job workflowdecl.Job) error {
	r.targets = r.runners.ContainerLimitations(job)
	r.guards = []string{job.If.Text()}
	r.reportUses(r.runners.JobContainerUses(job))
	return nil
}

func (r *containerAdapter) reportUses(uses []runnerlang.ContainerUse) {
	for _, finding := range ContainerRunnerFindings(uses) {
		r.Report(new(finding.Finding))
	}
}

func (r *containerAdapter) VisitStep(step workflowdecl.Step) error {
	r.conditions[step.Reference()] = step.If.Text()
	return r.checkStep(step)
}

func (r *containerAdapter) checkStep(step workflowdecl.Step) error {
	if len(r.targets) == 0 {
		return nil
	}
	action, ok := step.Action()
	if !ok || action.Uses == nil || action.Uses.ContainsExpression() {
		return nil
	}
	spec := action.Uses.Text()
	form := references.FormOf(spec)
	docker := form == references.FormContainer
	if !docker && (form == references.FormWorkspace || form == references.FormSelfRepository) {
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

func (r *containerAdapter) VisitInvocationStep(workflowdecl.Step) error { return nil }

func (r *containerAdapter) VisitOpaqueAction(step workflowdecl.Step) error {
	if len(r.frames) == 0 {
		return nil
	}
	return r.checkStep(step)
}
