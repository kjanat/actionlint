package builtin

import (
	"context"
	"fmt"
	"strings"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actionremote "actionlint.kjanat.dev/v2/internal/action/remote"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/source"
)

type containerFrame struct {
	context invocation.Context
	guards  int
}

type ruleContainerRunner struct {
	workflowvisit.Base
	ctx        context.Context
	runners    *runnerlang.Knowledge
	actions    *actionlocal.Resolver
	remote     *actionremote.Service
	targets    []runnerlang.ContainerTarget
	guards     []string
	frames     []containerFrame
	conditions map[source.Reference]string
}

func newRuleContainerRunner(c Context) *ruleContainerRunner {
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return &ruleContainerRunner{Base: builtinRuleBase("container-runner"), ctx: ctx, runners: c.Runners, actions: c.Actions, remote: c.RemoteActions, conditions: map[source.Reference]string{}}
}

func (r *ruleContainerRunner) VisitJobPre(job workflowdecl.JobView) error {
	r.targets = r.runners.ContainerLimitations(job)
	r.guards = []string{job.If().Value()}
	r.checkContainer(job.Container(), "job container")
	for name, service := range job.Services().Value() {
		r.checkContainer(service.Container(), "service "+name)
	}
	if expression := job.Services().Expression(); expression.Present() {
		for _, target := range r.targets {
			services := target.Evaluate(expression.Value())
			for name, service := range services.Props() {
				image, present := service.Prop("image")
				if text, known := image.ScalarString(); present && known && text != "" && r.enabled(target, "") {
					r.reportContainer(declarationPosition(expression), target, fmt.Sprintf("service %q", name))
				}
			}
		}
	}
	return nil
}

func (r *ruleContainerRunner) reportContainer(pos *workflowlang.Pos, target runnerlang.ContainerTarget, description string) {
	var finding *findingmodel.Finding
	switch target.Limitation {
	case runnerlang.ContainerRequiresLinux:
		finding = findingmodel.Errorf(pos, r.Name(), "%s requires a Linux runner with Docker. Runner selection %q resolves to a non-Linux runner", description, target.Runner)
		finding.Severity = "error"
	case runnerlang.ContainerMissingDaemon:
		finding = findingmodel.Errorf(pos, r.Name(), "%s requires a Docker daemon. Runner selection %q has Docker client tools but cannot run containers locally. Configure a compatible remote daemon or select a Docker-capable Linux runner", description, target.Runner)
		finding.Severity = "warning"
	case runnerlang.ContainerUnknown:
		return
	}
	if finding != nil {
		r.Report(finding)
	}
}

func (r *ruleContainerRunner) enabled(target runnerlang.ContainerTarget, condition string) bool {
	for _, guard := range append(append([]string{}, r.guards...), condition) {
		if guard == "" {
			continue
		}
		node, err, _ := exprlang.ParseConditionExpression(guard)
		if err == nil && execution.NeverRunsWithVariables(node, target.LookupExpressionVariable) {
			return false
		}
	}
	return true
}

func (r *ruleContainerRunner) checkContainer(container workflowdecl.ContainerView, description string) {
	if !container.Present() {
		return
	}
	for _, target := range r.targets {
		image := container.Image().Semantic()
		if container.Expression().Present() {
			image = target.Evaluate(container.Expression().Value())
			if objectImage, ok := image.Prop("image"); ok {
				image = objectImage
			}
		} else if container.Image().ContainsExpression() {
			image = target.Evaluate(container.Image().Value())
		}
		if name, known := image.ScalarString(); known && name != "" && r.enabled(target, "") {
			r.reportContainer(declarationPosition(container), target, description)
		}
	}
}

func (r *ruleContainerRunner) VisitStep(step workflowdecl.StepView) error {
	r.conditions[step.Reference()] = step.If().Value()
	return r.checkStep(step)
}

func (r *ruleContainerRunner) checkStep(step workflowdecl.StepView) error {
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
		docker = result.Status() == actionlocal.MetadataDockerfileOnly
		if meta := result.Metadata(); meta != nil {
			docker = strings.EqualFold(meta.Runs.Using, "docker")
		}
	} else if !docker {
		result, err := r.remote.Inspect(r.ctx, spec)
		if err != nil {
			return err
		}
		docker = strings.EqualFold(result.Definition().Using(), "docker")
	}
	if docker {
		for _, target := range r.targets {
			if r.enabled(target, step.If().Value()) {
				r.reportContainer(declarationPosition(action.Uses()), target, fmt.Sprintf("Docker action %q", spec))
			}
		}
	}
	return nil
}

func (r *ruleContainerRunner) ApplyCheckout(actions *actionlocal.Resolver) { r.actions = actions }

func (r *ruleContainerRunner) EnterInvocation(scope Invocation) error {
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

func (r *ruleContainerRunner) LeaveInvocation(scope invocation.Context) error {
	if len(r.frames) == 0 || r.frames[len(r.frames)-1].context != scope {
		return invocation.ErrForeignContext
	}
	frame := r.frames[len(r.frames)-1]
	r.guards = r.guards[:frame.guards]
	r.frames = r.frames[:len(r.frames)-1]
	return nil
}

func (r *ruleContainerRunner) VisitInvocationStep(workflowdecl.StepView) error { return nil }

func (r *ruleContainerRunner) VisitOpaqueAction(step workflowdecl.StepView) error {
	if len(r.frames) == 0 {
		return nil
	}
	return r.checkStep(step)
}
