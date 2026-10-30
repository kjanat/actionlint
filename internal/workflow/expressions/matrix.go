package expressions

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func (rule *Checker) checkMatrixSourceKey(source workflowdecl.StringView, evaluated bool) {
	if source.Present() && !evaluated {
		rule.checkString(source, "jobs.<job_id>.strategy")
	}
}

func (rule *Checker) checkMatrixExpression(expr workflowdecl.StringView) *exprlang.ObjectType {
	ty := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	return schema.MatrixType(ty)
}

func (rule *Checker) checkMatrix(m workflowdecl.MatrixView, resolution matrixcheck.Resolution) *exprlang.ObjectType {
	if !m.Present() {
		return exprlang.NewEmptyStrictObjectType()
	}
	inferred := map[diagnostic.Position]exprlang.Type{}
	if m.Expression().Present() {
		rule.checkMatrixExpression(m.Expression())
	} else {
		if m.Exclude().Present() {
			rule.checkMatrixSourceKey(m.Exclude().Name(), m.Exclude().Evaluated())
			if m.Exclude().Expression().Present() {
				if ty, ok := rule.checkArrayExpression(m.Exclude().Expression(), "exclude", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
					rule.checkObjectTy(ty.Elem, viewPosition(m.Exclude().Expression()), "exclude")
				}
			} else {
				for combination := range m.Exclude().Combinations() {
					rule.checkMatrixCombination(combination, "exclude")
				}
			}
		}
		for _, row := range m.Rows() {
			rule.checkMatrixSourceKey(row.Name(), row.Evaluated())
			ty := rule.checkMatrixRow(row)
			if row.Expression().Present() {
				if pos := viewPosition(row.Expression()); pos != nil {
					inferred[diagnostic.Position{Line: pos.Line, Column: pos.Col}] = ty
				}
			} else {
				for item := range row.Values() {
					if pos := rawValuePosition(item); pos != nil {
						inferred[diagnostic.Position{Line: pos.Line, Column: pos.Col}] = ty
					}
				}
			}
		}
		for insertion := range m.Insertions() {
			if insertion.Expression().Present() {
				rule.checkWorkflowExpression(insertion.Expression(), "matrix insertion", "jobs.<job_id>.strategy", schema.Matrix)
			} else {
				rule.checkMatrix(insertion, resolution)
			}
		}
		if m.Include().Present() {
			rule.checkMatrixSourceKey(m.Include().Name(), m.Include().Evaluated())
			if m.Include().Expression().Present() {
				rule.checkMatrixCombinationExpression(m.Include().Expression(), "include")
			} else {
				for combination := range m.Include().Combinations() {
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
	// Rows from an include expression are only known at runtime and may add
	// any property, as with include: ${{ fromJSON(inputs.targets) }}.
	if resolution.Open() || !resolution.IncludesComplete() {
		object.Loose()
	}
	return object
}

func (rule *Checker) checkMatrixCombination(combination workflowdecl.MatrixCombinationView, section string) {
	if !combination.Present() {
		return
	}
	if combination.Expression().Present() {
		rule.checkMatrixCombinationExpression(combination.Expression(), section)
		return
	}
	for _, assignment := range combination.Assigns() {
		if !assignment.Present() {
			continue
		}
		rule.checkMatrixSourceKey(assignment.Key(), combination.Evaluated())
		rule.checkRawYAMLValue(assignment.Value())
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
	case value.KindUnknown:
	}
	return exprlang.AnyType{}
}

func (rule *Checker) checkMatrixRow(r workflowdecl.MatrixRowView) exprlang.Type {
	rule.checkMatrixSourceKey(r.Name(), r.Evaluated())
	if r.Expression().Present() {
		if array, ok := rule.checkArrayExpression(r.Expression(), "matrix row", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			return array.Elem
		}
		return exprlang.AnyType{}
	}

	var element exprlang.Type
	for v := range r.Values() {
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

func (rule *Checker) checkMatrixCombinationExpression(s workflowdecl.StringView, section string) {
	ty := rule.checkOneExpression(s, "matrix combination at element of "+section+" section", "jobs.<job_id>.strategy")
	if array, ok := ty.(*exprlang.ArrayType); ok {
		if value, known := expressionLiteral(s.Value()); known {
			if values, ok := value.([]any); ok {
				// Merging object and scalar element types produces AnyType, so
				// validate each known value before using the inferred element type.
				invalid := false
				for i, value := range values {
					if _, ok := value.(map[string]any); !ok {
						rule.Errorf(viewPosition(s), "matrix combination in %s at inserted array index %d must be an object but got %s", section, i, exprlang.TypeOfJSONValue(value))
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
		rule.Errorf(viewPosition(s), "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
	}
}

func (rule *Checker) checkMatrixSequenceItem(value workflowdecl.RawYAMLValueView) exprlang.Type {
	ty := rule.checkRawYAMLValue(value)
	if scalar, ok := value.RawYAMLString(); ok && exprlang.ParseAssignedExpression(scalar.Value()) != nil {
		if array, ok := ty.(*exprlang.ArrayType); ok {
			if literal, known := expressionLiteral(scalar.Value()); known {
				if values, ok := literal.([]any); ok && len(values) == 0 {
					return nil
				}
			}
			return array.Elem // Template expansion inserts one level only.
		}
	}
	return ty
}
