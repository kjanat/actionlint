package runner

import (
	"strings"

	"actionlint.kjanat.dev/diagnostic"
	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/internal/workflow/matrix"
	"actionlint.kjanat.dev/internal/yaml"
	runnerapi "actionlint.kjanat.dev/runner"
)

// Selectors preserves conjunctions, groups, correlated alternatives and sources.
func Selectors(job *workflowlang.Job) []runnerapi.Selector {
	if job.RunsOn == nil {
		return nil
	}
	combinations, known := MatrixCombinations(job.Strategy)
	if !known {
		combinations = []runnerMatrixCombination{nil}
	}
	var selectors []runnerapi.Selector
	for _, combination := range combinations {
		selectors = append(selectors, selectorForRunner(job.RunsOn, combination))
	}
	// Retain known values from incomplete matrices without inventing correlations.
	if !known && job.Strategy != nil {
		for _, label := range append(append([]*workflowlang.String{}, job.RunsOn.Labels...), job.RunsOn.Expression, job.RunsOn.LabelsExpr) {
			if label == nil || runnerMatrixProperty(label) == "" {
				continue
			}
			property := runnerMatrixProperty(label)
			for _, value := range partialMatrixValues(job.Strategy, property) {
				s := selectorForRunner(job.RunsOn, runnerMatrixCombination{property: value})
				s.Incomplete = true
				selectors = append(selectors, s)
			}
		}
	}
	return selectors
}

func selectorForRunner(r *workflowlang.Runner, combination runnerMatrixCombination) runnerapi.Selector {
	s := runnerapi.Selector{}
	if r.Group != nil {
		group := selectorConstraint(r.Group, combination)
		s.Group = &group
	}
	switch {
	case r.Expression != nil:
		if raw, ok := selectorValue(r.Expression, combination); !ok || !fillSelector(&s, raw, runnerMatrixProperty(r.Expression) != "") {
			s = runnerapi.Selector{Expression: r.Expression.Value}
		}
	case r.LabelsExpr != nil:
		s.Labels = selectorLabels(r.LabelsExpr, combination)
	default:
		for _, label := range r.Labels {
			s.Labels = append(s.Labels, selectorLabels(label, combination)...)
		}
	}
	return s
}

func partialMatrixValues(strategy *workflowlang.Strategy, property string) []workflowlang.RawYAMLValue {
	m := strategy.Matrix
	if strategy.Expression != nil {
		m = matrixcheck.KnownStrategy(strategy.Expression)
	}
	if m == nil {
		return nil
	}
	m, _ = matrixcheck.Expand(m)
	if m == nil {
		return nil
	}
	var values []workflowlang.RawYAMLValue
	appendValue := func(value workflowlang.RawYAMLValue, expressions bool) {
		if !expressions || !exprlang.ContainsExpression(value.String()) {
			values = append(values, value)
		}
	}
	if row := m.Rows[property]; row != nil {
		elements, _ := matrixcheck.RowElements(row, true)
		for _, element := range elements {
			appendValue(element.Value, element.Expressions)
		}
	}
	for _, combination := range matrixcheck.Combinations(m.Include, true) {
		if assign := combination.Combination.Assigns[property]; assign != nil {
			appendValue(assign.Value, combination.Expressions)
		}
	}
	return values
}

func literalConstraint(text string, pos *workflowlang.Pos) runnerapi.Constraint {
	var position diagnostic.Position
	if pos != nil {
		position = diagnostic.Position{Line: pos.Line, Column: pos.Col}
	}
	return runnerapi.Constraint{Value: runnerapi.Literal(text), Position: position}
}

func selectorConstraint(value *workflowlang.String, combination runnerMatrixCombination) runnerapi.Constraint {
	if !value.ContainsExpression() {
		return literalConstraint(value.Value, value.Pos)
	}
	if raw, known := selectorValue(value, combination); known {
		if scalar, ok := raw.(*workflowlang.RawYAMLString); ok && scalar.ScalarValue() != nil {
			if text, ok := workflowlang.WorkflowScalarString(scalar.ScalarValue()); ok {
				return literalConstraint(text, scalar.Pos())
			}
		}
	}
	c := literalConstraint(value.Value, value.Pos)
	c.Value = runnerapi.Expression(value.Value)
	return c
}

func selectorLabels(value *workflowlang.String, combination runnerMatrixCombination) []runnerapi.Constraint {
	if raw, known := selectorValue(value, combination); known {
		if constraints, valid := rawConstraints(raw, runnerMatrixProperty(value) != ""); valid {
			return constraints
		}
	}
	return []runnerapi.Constraint{selectorConstraint(value, combination)}
}

func selectorValue(value *workflowlang.String, combination runnerMatrixCombination) (workflowlang.RawYAMLValue, bool) {
	if property := runnerMatrixProperty(value); property != "" {
		if raw, ok := combination[property]; ok {
			return raw, true
		}
	}
	literal, known := workflowlang.WorkflowExpressionLiteral(value)
	if !known {
		return nil, false
	}
	var node yaml.Node
	if err := node.Encode(literal); err != nil {
		return nil, false
	}
	var position func(*yaml.Node)
	position = func(n *yaml.Node) {
		if value.Pos != nil {
			n.Line, n.Column = value.Pos.Line, value.Pos.Col
		}
		for _, child := range n.Content {
			position(child)
		}
	}
	position(&node)
	return (&workflowlang.Parser{}).ParseRawYAMLValue(&node), true
}

func rawConstraints(raw workflowlang.RawYAMLValue, nullIsLabel bool) ([]runnerapi.Constraint, bool) {
	var result []runnerapi.Constraint
	if array, ok := raw.(*workflowlang.RawYAMLArray); ok {
		for _, child := range array.Elems {
			constraint, valid := rawConstraint(child, nullIsLabel)
			if !valid {
				return nil, false
			}
			result = append(result, constraint)
		}
		return result, true
	}
	constraint, valid := rawConstraint(raw, nullIsLabel)
	if !valid {
		return nil, false
	}
	return []runnerapi.Constraint{constraint}, true
}

func rawConstraint(raw workflowlang.RawYAMLValue, nullIsLabel bool) (runnerapi.Constraint, bool) {
	if scalar, ok := raw.(*workflowlang.RawYAMLString); ok && (nullIsLabel || scalar.ScalarValue() != nil) {
		if text, valid := workflowlang.WorkflowScalarString(scalar.ScalarValue()); valid {
			return literalConstraint(text, scalar.Pos()), true
		}
	}
	return runnerapi.Constraint{}, false
}

func fillSelector(s *runnerapi.Selector, raw workflowlang.RawYAMLValue, nullIsLabel bool) bool {
	if object, ok := raw.(*workflowlang.RawYAMLObject); ok {
		seen := make(map[string]bool)
		for name, value := range object.Props {
			name = strings.ToLower(name)
			if seen[name] {
				return false
			}
			seen[name] = true
			switch name {
			case "labels":
				labels, valid := rawConstraints(value, nullIsLabel)
				if !valid {
					return false
				}
				s.Labels = labels
			case "group":
				group, valid := rawConstraint(value, false)
				if !valid {
					return false
				}
				s.Group = &group
			default:
				return false
			}
		}
		return len(seen) != 0
	}
	labels, valid := rawConstraints(raw, nullIsLabel)
	s.Labels = labels
	return valid
}
