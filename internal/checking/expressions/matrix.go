package expressions

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	matrixcheck "actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

func (rule *Checker) checkMatrixSourceKey(source workflowdecl.StringView, evaluated bool) {
	if source.Present() && !evaluated {
		rule.checkString(source, "jobs.<job_id>.strategy")
	}
}

// elementTypes merges the types of array elements in the order they are
// added.
type elementTypes struct {
	ty   exprtypes.Type
	seen bool
}

func (e *elementTypes) add(t exprtypes.Type) {
	if !e.seen {
		e.ty, e.seen = t, true
		return
	}
	e.ty = e.ty.Merge(t)
}

// merged returns the merged element type, or any when no element was added.
func (e *elementTypes) merged() exprtypes.Type {
	if !e.seen {
		return exprtypes.AnyType()
	}
	return e.ty
}

func (rule *Checker) checkMatrixExpression(expr workflowdecl.StringView) exprtypes.ObjectType {
	ty, ok := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	if !ok {
		return exprtypes.NewEmptyObjectType()
	}
	return schema.MatrixType(ty)
}

func (rule *Checker) checkMatrix(m workflowdecl.MatrixView, resolution matrixcheck.Resolution) exprtypes.ObjectType {
	if !m.Present() {
		return exprtypes.NewEmptyStrictObjectType()
	}
	// inferred holds the element type of each row declaration. A missing
	// position reads as any, which means nothing was inferred.
	inferred := map[sourcepos.Position]exprtypes.Type{}
	if m.Expression().Present() {
		rule.checkMatrixExpression(m.Expression())
	} else {
		if m.Exclude().Present() {
			rule.checkMatrixSourceKey(m.Exclude().Name(), m.Exclude().Evaluated())
			if m.Exclude().Expression().Present() {
				if ty, ok := rule.checkArrayExpression(m.Exclude().Expression(), "exclude", "jobs.<job_id>.strategy"); ok {
					if array, ok := ty.Array(); ok {
						rule.checkObjectTy(array.Elem(), viewPosition(m.Exclude().Expression()), "exclude")
					}
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
					inferred[sourcepos.Position{Line: pos.Line, Column: pos.Col}] = ty
				}
			} else {
				for item := range row.Values() {
					if pos := rawValuePosition(item); pos != nil {
						inferred[sourcepos.Position{Line: pos.Line, Column: pos.Col}] = ty
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

func matrixTypeFromResolution(resolution matrixcheck.Resolution) exprtypes.ObjectType {
	return matrixTypeFromResolutionWithInferredRows(resolution, nil)
}

func matrixTypeFromResolutionWithInferredRows(resolution matrixcheck.Resolution, inferred map[sourcepos.Position]exprtypes.Type) exprtypes.ObjectType {
	if !resolution.IncludesComplete() || resolution.Open() {
		inferred = nil
	}
	var object exprtypes.ObjectBuilder
	for name := range resolution.ReachableNames() {
		reach := resolution.Reachable(name)
		var values elementTypes
		for _, candidate := range reach.Values {
			// Only row declarations supply this inference. An unknown include
			// assignment must not inherit an axis's more specific element type.
			values.add(matrixValueType(candidate, inferred[candidate.Origin().Span.Start]))
		}
		ty := values.merged()
		if !resolution.IncludesComplete() {
			ty = exprtypes.AnyType()
		}
		object.Set(name, ty)
	}
	// Rows from an include expression are only known at runtime and may add
	// any property, as with include: ${{ fromJSON(inputs.targets) }}.
	if resolution.Open() || !resolution.IncludesComplete() {
		object.SetMapped(exprtypes.AnyType())
	}
	return object.Build()
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

// matrixValueType types a matrix value. inferred is the type a row expression
// gave the value; any means nothing was inferred.
func matrixValueType(item value.Value, inferred exprtypes.Type) exprtypes.Type {
	if !item.Known() {
		return inferred
	}
	switch item.Kind() {
	case value.Null:
		return exprtypes.NullType()
	case value.Bool:
		return exprtypes.BoolType()
	case value.Number:
		return exprtypes.NumberType()
	case value.String:
		return exprtypes.StringType()
	case value.Object:
		object, _ := inferred.Object()
		properties := map[string]exprtypes.Type{}
		for name, property := range item.Props() {
			// An undeclared property reads as any: nothing was inferred.
			propertyType, _ := object.Property(name)
			properties[name] = matrixValueType(property, propertyType)
		}
		return exprtypes.NewStrictObjectType(properties).Type()
	case value.Array:
		array, _ := inferred.Array()
		var elements elementTypes
		for child := range item.Items() {
			elements.add(matrixValueType(child, array.Elem()))
		}
		return exprtypes.NewArrayType(elements.merged()).Type()
	case value.KindUnknown:
	}
	return exprtypes.AnyType()
}

// checkMatrixRow returns the element type of a matrix row, or any when no
// element has a known type.
func (rule *Checker) checkMatrixRow(r workflowdecl.MatrixRowView) exprtypes.Type {
	rule.checkMatrixSourceKey(r.Name(), r.Evaluated())
	if r.Expression().Present() {
		if ty, ok := rule.checkArrayExpression(r.Expression(), "matrix row", "jobs.<job_id>.strategy"); ok {
			if array, ok := ty.Array(); ok {
				return array.Elem()
			}
		}
		return exprtypes.AnyType()
	}

	var elements elementTypes
	for v := range r.Values() {
		if ty, ok := rule.checkMatrixSequenceItem(v); ok {
			elements.add(ty)
		}
	}
	return elements.merged()
}

func (rule *Checker) checkMatrixCombinationExpression(s workflowdecl.StringView, section string) {
	ty, ok := rule.checkOneExpression(s, "matrix combination at element of "+section+" section", "jobs.<job_id>.strategy")
	if !ok {
		return
	}
	if array, ok := ty.Array(); ok {
		if value, known := expressionLiteral(s.Value()); known {
			if values, ok := value.([]any); ok {
				// Merging object and scalar element types produces AnyType, so
				// validate each known value before using the inferred element type.
				invalid := false
				for i, value := range values {
					if _, ok := value.(map[string]any); !ok {
						rule.Errorf(viewPosition(s), "matrix combination in %s at inserted array index %d must be an object but got %s", section, i, exprtypes.TypeOfJSONValue(value))
						invalid = true
					}
				}
				if invalid || len(values) == 0 {
					return
				}
			}
		}
		ty = array.Elem()
	}
	if k := ty.Kind(); k != exprtypes.KindAny && k != exprtypes.KindObject {
		rule.Errorf(viewPosition(s), "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
	}
}

// checkMatrixSequenceItem returns the type of the values a sequence item adds.
// ok is false when the item is an expression that inserts no values, such as
// an empty array literal.
func (rule *Checker) checkMatrixSequenceItem(value workflowdecl.RawYAMLValueView) (ty exprtypes.Type, ok bool) {
	ty = rule.checkRawYAMLValue(value)
	if scalar, ok := value.RawYAMLString(); ok && exprsyntax.ParseAssignedExpression(scalar.Value()) != nil {
		if array, ok := ty.Array(); ok {
			if literal, known := expressionLiteral(scalar.Value()); known {
				if values, ok := literal.([]any); ok && len(values) == 0 {
					return exprtypes.AnyType(), false
				}
			}
			return array.Elem(), true // Template expansion inserts one level only.
		}
	}
	return ty, true
}
