package runners

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
)

type ContainerUseKind uint8

const (
	JobContainer ContainerUseKind = iota
	ServiceContainer
	DockerAction
)

// ContainerUse pairs a declared container operation with a runner alternative
// on which its conditions have not been proved false.
type ContainerUse struct {
	Target   ContainerTarget
	Kind     ContainerUseKind
	Name     string
	Position sourcepos.Position
}

// MayRun excludes an alternative only when a condition is provably false.
// Invalid conditions and unknown runtime values cannot establish exclusion.
func (target ContainerTarget) MayRun(conditions ...string) bool {
	for _, condition := range conditions {
		if condition == "" {
			continue
		}
		node, err, _ := exprsyntax.ParseConditionExpression(condition)
		if err == nil && workflowconditions.NeverRunsWithVariables(node, target.LookupExpressionVariable) {
			return false
		}
	}
	return true
}

func containerUse(target ContainerTarget, kind ContainerUseKind, name string, pos *workflowlang.Pos) ContainerUse {
	use := ContainerUse{Target: target, Kind: kind, Name: name}
	if pos != nil {
		use.Position = sourcepos.Position{Line: pos.Line, Column: pos.Col}
	}
	return use
}

// JobContainerUses evaluates job containers and services using established
// runner and matrix facts. It does not read action metadata or acquire sources.
func (k *Knowledge) JobContainerUses(job workflowdecl.Job) []ContainerUse {
	targets := k.ContainerLimitations(job)
	var uses []ContainerUse
	add := func(container *workflowlang.Container, kind ContainerUseKind, name string) {
		if container == nil {
			return
		}
		for _, target := range targets {
			image := workflowdecl.Scalar(container.Image)
			if container.Expression != nil {
				image = target.Evaluate(container.Expression.Value)
				if objectImage, ok := image.Prop("image"); ok {
					image = objectImage
				}
			} else if container.Image.ContainsExpression() {
				image = target.Evaluate(container.Image.Value)
			}
			if text, known := image.ScalarString(); known && text != "" && target.MayRun(job.If.Text()) {
				uses = append(uses, containerUse(target, kind, name, container.Pos))
			}
		}
	}
	add(job.Container, JobContainer, "")
	if job.Services == nil {
		return uses
	}
	for name, service := range job.Services.Value {
		if service != nil {
			add(service.Container, ServiceContainer, name)
		}
	}
	if expression := job.Services.Expression; expression != nil {
		for _, target := range targets {
			services := target.Evaluate(expression.Value)
			for name, service := range services.Props() {
				image, present := service.Prop("image")
				if text, known := image.ScalarString(); present && known && text != "" && target.MayRun(job.If.Text()) {
					uses = append(uses, containerUse(target, ServiceContainer, name, expression.Pos))
				}
			}
		}
	}
	return uses
}

// DockerActionUses supplies execution alternatives after the caller has
// established that the action uses Docker. Guards belong to its enclosing calls.
func DockerActionUses(step workflowdecl.Step, targets []ContainerTarget, guards []string) []ContainerUse {
	action, present := step.Action()
	if !present {
		return nil
	}
	var uses []ContainerUse
	for _, target := range targets {
		if target.MayRun(guards...) && target.MayRun(step.If.Text()) {
			var pos *workflowlang.Pos
			if action.Uses != nil {
				pos = action.Uses.Pos
			}
			uses = append(uses, containerUse(target, DockerAction, action.Uses.Text(), pos))
		}
	}
	return uses
}
