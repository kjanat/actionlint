package runner

import (
	"slices"

	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func Selectors(job workflowdecl.JobView) []runnerapi.Selector {
	if !job.Present() || !job.RunsOn().Present() {
		return nil
	}
	selection := job.RunsOn().Selection()
	if !hasMatrixReference(selection) {
		return []runnerapi.Selector{selectorFor(selection, matrix.Combination{}, false)}
	}
	strategy := job.Strategy().MatrixResolution()
	combinations, complete := strategy.Combinations(256)
	if complete {
		if len(combinations) == 0 {
			combinations = []matrix.Combination{{}}
		}
		result := make([]runnerapi.Selector, 0, len(combinations))
		for _, combination := range combinations {
			result = append(result, selectorFor(selection, combination, false))
		}
		return result
	}
	result := []runnerapi.Selector{selectorFor(selection, matrix.Combination{}, true)}
	for _, candidate := range selectionValues(selection) {
		property := runnerMatrixProperty(candidate.Source())
		if property == "" {
			continue
		}
		reachable := strategy.ReachableAfterExcludes(property)
		for _, item := range reachable.Values {
			result = append(result, selectorFor(selection, (matrix.Combination{}).With(property, item), true))
		}
	}
	return result
}

func selectorFor(selection value.Value, combination matrix.Combination, incomplete bool) runnerapi.Selector {
	result := runnerapi.Selector{Incomplete: incomplete}
	candidate := selection
	if selection.Known() && selection.Kind() == value.Object {
		if group, ok := selection.Prop("group"); ok {
			constraint := groupConstraint(group, combination)
			result.Group = &constraint
			if _, unresolved := constraint.Value.(runnerapi.Expression); unresolved {
				result.Incomplete = true
			}
		}
		labels, ok := selection.Prop("labels")
		if !ok {
			result.Incomplete = result.Incomplete || result.Group == nil
			return result
		}
		candidate = labels
	}
	resolved, ok := resolveMatrixValue(candidate, combination)
	if !ok {
		result.Incomplete = true
		if selection.Kind() == value.Object {
			result.Labels = []runnerapi.Constraint{selectorConstraint(candidate, combination)}
		} else {
			result.Expression = candidate.Source()
		}
		return result
	}
	if resolved.Known() && resolved.Kind() == value.Object {
		if result.Group == nil {
			if group, ok := resolved.Prop("group"); ok {
				constraint := groupConstraint(group, combination)
				result.Group = &constraint
				if _, unresolved := constraint.Value.(runnerapi.Expression); unresolved {
					result.Incomplete = true
				}
			}
		}
	}
	constraints, valid := constraintsFor(resolved, hasMatrixReference(candidate), combination)
	if !valid {
		result.Incomplete = true
		if selection.Kind() == value.Object {
			result.Labels = []runnerapi.Constraint{selectorConstraint(candidate, combination)}
		} else {
			result.Expression = candidate.Source()
		}
		return result
	}
	result.Labels = constraints
	if len(constraints) == 0 && result.Group == nil {
		result.Incomplete = true
	}
	for _, constraint := range constraints {
		if _, unresolved := constraint.Value.(runnerapi.Expression); unresolved {
			result.Incomplete = true
		}
	}
	return result
}

func selectorConstraint(source value.Value, combination matrix.Combination) runnerapi.Constraint {
	resolved, ok := resolveMatrixValue(source, combination)
	if ok {
		if text, scalar := resolved.ScalarString(); scalar {
			return runnerapi.Constraint{Value: runnerapi.Literal(text), Position: resolved.Origin().Span.Start}
		}
	}
	constraint := runnerapi.Constraint{Value: runnerapi.Expression(source.Source()), Position: source.Origin().Span.Start}
	return constraint
}

func groupConstraint(source value.Value, combination matrix.Combination) runnerapi.Constraint {
	if source.Kind() == value.Null {
		return runnerapi.Constraint{Value: runnerapi.Expression(source.Source()), Position: source.Origin().Span.Start}
	}
	return selectorConstraint(source, combination)
}

func resolveMatrixValue(source value.Value, combination matrix.Combination) (value.Value, bool) {
	if property := runnerMatrixProperty(source.Source()); property != "" {
		if item, ok := combination.Get(property); ok {
			return item, true
		}
	}
	if source.Known() {
		return source, true
	}
	return value.Value{}, false
}

func constraintsFor(candidate value.Value, nullIsLabel bool, combination matrix.Combination) ([]runnerapi.Constraint, bool) {
	if candidate.Known() && candidate.Kind() == value.Object {
		labels, ok := candidate.Prop("labels")
		if !ok {
			return nil, false
		}
		candidate = labels
	}
	var values []value.Value
	if candidate.Known() && candidate.Kind() == value.Array {
		values = slices.Collect(candidate.Items())
	} else {
		values = []value.Value{candidate}
	}
	result := make([]runnerapi.Constraint, 0, len(values))
	for _, item := range values {
		if item.Kind() == value.Null && !nullIsLabel {
			continue
		}
		resolved, ok := resolveMatrixValue(item, combination)
		if !ok {
			result = append(result, selectorConstraint(item, combination))
			continue
		}
		text, ok := resolved.ScalarString()
		if !ok {
			result = append(result, selectorConstraint(item, combination))
			continue
		}
		result = append(result, runnerapi.Constraint{Value: runnerapi.Literal(text), Position: resolved.Origin().Span.Start})
	}
	return result, true
}

func selectionValues(selection value.Value) []value.Value {
	candidate := selection
	if selection.Known() && selection.Kind() == value.Object {
		labels, ok := selection.Prop("labels")
		if !ok {
			return nil
		}
		candidate = labels
	}
	if candidate.Known() && candidate.Kind() == value.Array {
		return slices.Collect(candidate.Items())
	}
	if candidate.State() == value.Missing {
		return nil
	}
	return []value.Value{candidate}
}
