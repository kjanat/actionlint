package runner

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

func containerUse(target ContainerTarget, kind ContainerUseKind, name string, position interface {
	Pos() (workflowlang.Pos, bool)
},
) ContainerUse {
	pos, present := position.Pos()
	use := ContainerUse{Target: target, Kind: kind, Name: name}
	if present {
		use.Position = sourcepos.Position{Line: pos.Line, Column: pos.Col}
	}
	return use
}

// JobContainerUses evaluates job containers and services using established
// runner and matrix facts. It does not read action metadata or acquire sources.
func (k *Knowledge) JobContainerUses(job workflowdecl.JobView) []ContainerUse {
	targets := k.ContainerLimitations(job)
	var uses []ContainerUse
	add := func(container workflowdecl.ContainerView, kind ContainerUseKind, name string) {
		if !container.Present() {
			return
		}
		for _, target := range targets {
			image := container.Image().Semantic()
			if container.Expression().Present() {
				image = target.Evaluate(container.Expression().Value())
				if objectImage, ok := image.Prop("image"); ok {
					image = objectImage
				}
			} else if container.Image().ContainsExpression() {
				image = target.Evaluate(container.Image().Value())
			}
			if text, known := image.ScalarString(); known && text != "" && target.MayRun(job.If().Value()) {
				uses = append(uses, containerUse(target, kind, name, container))
			}
		}
	}
	add(job.Container(), JobContainer, "")
	for name, service := range job.Services().Value() {
		add(service.Container(), ServiceContainer, name)
	}
	if expression := job.Services().Expression(); expression.Present() {
		for _, target := range targets {
			services := target.Evaluate(expression.Value())
			for name, service := range services.Props() {
				image, present := service.Prop("image")
				if text, known := image.ScalarString(); present && known && text != "" && target.MayRun(job.If().Value()) {
					uses = append(uses, containerUse(target, ServiceContainer, name, expression))
				}
			}
		}
	}
	return uses
}

// DockerActionUses supplies execution alternatives after the caller has
// established that the action uses Docker. Guards belong to its enclosing calls.
func DockerActionUses(step workflowdecl.StepView, targets []ContainerTarget, guards []string) []ContainerUse {
	action, present := step.Action()
	if !present {
		return nil
	}
	var uses []ContainerUse
	for _, target := range targets {
		if target.MayRun(guards...) && target.MayRun(step.If().Value()) {
			uses = append(uses, containerUse(target, DockerAction, action.Uses().Value(), action.Uses()))
		}
	}
	return uses
}
