package execution

import (
	"bytes"
	"slices"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func copyString(value *workflowlang.String) *workflowlang.String {
	cloned := copyValue(value)
	if cloned != nil {
		cloned.Pos = copyValue(value.Pos)
	}
	return cloned
}

func copySource(source *workflowlang.ScriptSource) *workflowlang.ScriptSource {
	cloned := copyValue(source)
	if cloned != nil {
		cloned.Lines = slices.Clone(source.Lines)
		cloned.Spans = slices.Clone(source.Spans)
		cloned.Comments = slices.Clone(source.Comments)
	}
	return cloned
}

func cloneInputs(scopes []*Scope, units []Unit) ([]*Scope, []Unit) {
	clonedScopes := make([]*Scope, len(scopes))
	byScope := make(map[*Scope]*Scope, len(scopes))
	placements := map[*workflowexecution.CheckoutPlacement]*workflowexecution.CheckoutPlacement{}
	for i, scope := range scopes {
		clonedScopes[i] = copyValue(scope)
		for placement := scope.Paths.Placements; placement != nil && placements[placement] == nil; placement = placement.Previous {
			placements[placement] = copyValue(placement)
		}
		clonedScopes[i].Paths.Placements = placements[scope.Paths.Placements]
		byScope[scope] = clonedScopes[i]
	}
	for original, cloned := range placements {
		cloned.Previous = placements[original.Previous]
	}
	clonedUnits := slices.Clone(units)
	// Repeated invocations share one copy of each supplied document buffer.
	contents := make(map[*byte][]byte)
	for i := range clonedUnits {
		unit := &clonedUnits[i]
		unit.Scope = byScope[unit.Scope]
		unit.Shell = copyValue(unit.Shell)
		unit.Owner = copyValue(unit.Owner)
		if unit.Owner != nil {
			unit.Owner.Step = copyValue(unit.Owner.Step)
		}
		unit.Origin = copyValue(unit.Origin)
		if unit.Origin != nil {
			unit.Origin.StepPosition = copyValue(unit.Origin.StepPosition)
		}
		if len(unit.Content) != 0 {
			key := &unit.Content[0]
			if content, exists := contents[key]; exists && len(content) == len(unit.Content) {
				unit.Content = content
			} else {
				unit.Content = bytes.Clone(unit.Content)
				contents[key] = unit.Content
			}
		}
		if unit.Run != nil {
			unit.Run = copyValue(unit.Run)
			unit.Run.Run = copyString(unit.Run.Run)
			unit.Run.Shell = copyString(unit.Run.Shell)
			unit.Run.WorkingDirectory = copyString(unit.Run.WorkingDirectory)
			unit.Run.RunPos = copyValue(unit.Run.RunPos)
			unit.Run.Source = copySource(unit.Run.Source)
		}
		if unit.Input != nil {
			unit.Input = copyValue(unit.Input)
			unit.Input.Value = copyValue(unit.Input.Value)
			unit.Input.Value.Name = copyString(unit.Input.Value.Name)
			unit.Input.Value.Value = copyString(unit.Input.Value.Value)
			unit.Input.Value.Source = copySource(unit.Input.Value.Source)
		}
	}
	return clonedScopes, clonedUnits
}
