package expressions

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func (rule *Checker) checkMatrixSourceKey(source *workflowlang.String, evaluated bool) {
	if source != nil && !evaluated {
		rule.checkString(source, "jobs.<job_id>.strategy")
	}
}

func (rule *Checker) checkMatrixExpression(expr *workflowlang.String) *exprlang.ObjectType {
	ty := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	return schema.MatrixType(ty)
}

func (rule *Checker) checkMatrix(m *workflowlang.Matrix, resolution matrixcheck.Resolution) *exprlang.ObjectType {
	if m == nil {
		return exprlang.NewEmptyStrictObjectType()
	}
	inferred := map[diagnostic.Position]exprlang.Type{}
	if m.Expression != nil {
		rule.checkMatrixExpression(m.Expression)
	} else {
		if m.Exclude != nil {
			rule.checkMatrixSourceKey(m.Exclude.Name, m.Exclude.Evaluated)
			if m.Exclude.Expression != nil {
				if ty, ok := rule.checkArrayExpression(m.Exclude.Expression, "exclude", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
					rule.checkObjectTy(ty.Elem, m.Exclude.Expression.Pos, "exclude")
				}
			} else {
				for _, combination := range m.Exclude.Combinations {
					rule.checkMatrixCombination(combination, "exclude")
				}
			}
		}
		for _, row := range m.Rows {
			rule.checkMatrixSourceKey(row.Name, row.Evaluated)
			ty := rule.checkMatrixRow(row)
			if row.Expression != nil {
				if pos := row.Expression.Pos; pos != nil {
					inferred[diagnostic.Position{Line: pos.Line, Column: pos.Col}] = ty
				}
			} else {
				for _, item := range row.Values {
					if pos := item.Pos(); pos != nil {
						inferred[diagnostic.Position{Line: pos.Line, Column: pos.Col}] = ty
					}
				}
			}
		}
		for _, insertion := range m.Insertions {
			if insertion.Expression != nil {
				rule.checkWorkflowExpression(insertion.Expression, "matrix insertion", "jobs.<job_id>.strategy", schema.Matrix)
			} else {
				rule.checkMatrix(insertion, resolution)
			}
		}
		if m.Include != nil {
			rule.checkMatrixSourceKey(m.Include.Name, m.Include.Evaluated)
			if m.Include.Expression != nil {
				rule.checkMatrixCombinationExpression(m.Include.Expression, "include")
			} else {
				for _, combination := range m.Include.Combinations {
					rule.checkMatrixCombination(combination, "include")
				}
			}
		}
	}

	return matrixTypeFromResolutionWithInferredRows(resolution, inferred)
}

func matrixTypeFromResolution(resolution matrixcheck.Resolution) *exprlang.ObjectType {
	return matrixTypeFromResolutionWithInferredRows(resolution, nil)
}

func matrixTypeFromResolutionWithInferredRows(resolution matrixcheck.Resolution, inferred map[diagnostic.Position]exprlang.Type) *exprlang.ObjectType {
	if !resolution.IncludesComplete() || resolution.Open() {
		inferred = nil
	}
	object := exprlang.NewEmptyStrictObjectType()
	for name := range resolution.ReachableNames() {
		reach := resolution.Reachable(name)
		var ty exprlang.Type
		for _, candidate := range reach.Values {
			// Only row declarations supply this inference. An unknown include
			// assignment must not inherit an axis's more specific element type.
			candidateType := matrixValueType(candidate, inferred[candidate.Origin().Span.Start])
			if ty == nil {
				ty = candidateType
			} else {
				ty = ty.Merge(candidateType)
			}
		}
		if ty == nil || !resolution.IncludesComplete() {
			ty = exprlang.AnyType{}
		}
		object.Props[name] = ty
	}
	if resolution.Open() {
		object.Loose()
	}
	return object
}

func (rule *Checker) checkMatrixCombination(combination *workflowlang.MatrixCombination, section string) {
	if combination == nil {
		return
	}
	if combination.Expression != nil {
		rule.checkMatrixCombinationExpression(combination.Expression, section)
		return
	}
	for _, assignment := range combination.Assigns {
		if assignment == nil {
			continue
		}
		rule.checkMatrixSourceKey(assignment.Key, combination.Evaluated)
		rule.checkRawYAMLValue(assignment.Value)
	}
}

func matrixValueType(item value.Value, inferred exprlang.Type) exprlang.Type {
	if !item.Known() {
		if inferred != nil {
			return inferred
		}
		return exprlang.AnyType{}
	}
	switch item.Kind() {
	case value.Null:
		return exprlang.NullType{}
	case value.Bool:
		return exprlang.BoolType{}
	case value.Number:
		return exprlang.NumberType{}
	case value.String:
		return exprlang.StringType{}
	case value.Object:
		properties := map[string]exprlang.Type{}
		for name, property := range item.Props() {
			var propertyType exprlang.Type
			if object, ok := inferred.(*exprlang.ObjectType); ok {
				propertyType = object.Props[name]
			}
			properties[name] = matrixValueType(property, propertyType)
		}
		return exprlang.NewStrictObjectType(properties)
	case value.Array:
		var element exprlang.Type
		for child := range item.Items() {
			var elementType exprlang.Type
			if array, ok := inferred.(*exprlang.ArrayType); ok {
				elementType = array.Elem
			}
			t := matrixValueType(child, elementType)
			if element == nil {
				element = t
			} else {
				element = element.Merge(t)
			}
		}
		if element == nil {
			element = exprlang.AnyType{}
		}
		return &exprlang.ArrayType{Elem: element, Deref: false}
	default:
		return exprlang.AnyType{}
	}
}

func (rule *Checker) checkMatrixRow(r *workflowlang.MatrixRow) exprlang.Type {
	rule.checkMatrixSourceKey(r.Name, r.Evaluated)
	if r.Expression != nil {
		if array, ok := rule.checkArrayExpression(r.Expression, "matrix row", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			return array.Elem
		}
		return exprlang.AnyType{}
	}

	var element exprlang.Type
	for _, v := range r.Values {
		if ty := rule.checkMatrixSequenceItem(v); ty != nil {
			if element == nil {
				element = ty
			} else {
				element = element.Merge(ty)
			}
		}
	}
	return element
}

func (rule *Checker) checkMatrixCombinationExpression(s *workflowlang.String, section string) {
	ty := rule.checkOneExpression(s, "matrix combination at element of "+section+" section", "jobs.<job_id>.strategy")
	if array, ok := ty.(*exprlang.ArrayType); ok {
		if value, known := workflowlang.WorkflowExpressionLiteral(s); known {
			if values, ok := value.([]any); ok {
				// Merging object and scalar element types produces AnyType, so
				// validate each known value before using the inferred element type.
				invalid := false
				for i, value := range values {
					if _, ok := value.(map[string]any); !ok {
						rule.Errorf(s.Pos, "matrix combination in %s at inserted array index %d must be an object but got %s", section, i, exprlang.TypeOfJSONValue(value))
						invalid = true
					}
				}
				if invalid || len(values) == 0 {
					return
				}
			}
		}
		ty = array.Elem
	}
	switch ty.(type) {
	case nil, exprlang.AnyType, *exprlang.ObjectType:
		return
	default:
		rule.Errorf(s.Pos, "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
	}
}

func (rule *Checker) checkMatrixSequenceItem(value workflowlang.RawYAMLValue) exprlang.Type {
	ty := rule.checkRawYAMLValue(value)
	if scalar, ok := value.(*workflowlang.RawYAMLString); ok && exprlang.ParseAssignedExpression(scalar.Value) != nil {
		if array, ok := ty.(*exprlang.ArrayType); ok {
			if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value}); known {
				if values, ok := literal.([]any); ok && len(values) == 0 {
					return nil
				}
			}
			return array.Elem // Template expansion inserts one level only.
		}
	}
	return ty
}
