package expressions

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

func (rule *Visitor) checkMatrixSourceKey(source *definition.String, evaluated bool) {
	if source != nil && !evaluated {
		rule.checkString(source, "jobs.<job_id>.strategy")
	}
}

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

func (e *elementTypes) merged() exprtypes.Type {
	if !e.seen {
		return exprtypes.AnyType()
	}
	return e.ty
}

func (rule *Visitor) checkMatrixExpression(expr *definition.String) exprtypes.ObjectType {
	ty, ok := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	if !ok {
		return exprtypes.NewEmptyObjectType()
	}
	return schema.MatrixType(ty)
}

func (rule *Visitor) checkMatrix(m *definition.Matrix, resolution matrix.Resolution) exprtypes.ObjectType {
	if m == nil {
		return exprtypes.NewEmptyStrictObjectType()
	}
	// A missing position reads as any.
	inferred := map[sourcetext.Position]exprtypes.Type{}
	if m.Expression != nil {
		rule.checkMatrixExpression(m.Expression)
	} else {
		if m.Exclude != nil {
			rule.checkMatrixSourceKey(m.Exclude.Name, m.Exclude.Evaluated)
			if m.Exclude.Expression != nil {
				if ty, ok := rule.checkArrayExpression(m.Exclude.Expression, "exclude", "jobs.<job_id>.strategy"); ok {
					if array, ok := ty.Array(); ok {
						rule.checkObjectTy(array.Elem(), scalarPosition(m.Exclude.Expression), "exclude")
					}
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
				if pos := scalarPosition(row.Expression); pos != nil {
					inferred[*pos] = ty
				}
			} else {
				for _, item := range row.Values {
					if pos := rawValuePosition(item); pos != nil {
						inferred[*pos] = ty
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

	return resolution.ContextType(inferred)
}

func (rule *Visitor) checkMatrixCombination(combination *definition.MatrixCombination, section string) {
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

func (rule *Visitor) checkMatrixRow(r *definition.MatrixRow) exprtypes.Type {
	rule.checkMatrixSourceKey(r.Name, r.Evaluated)
	if r.Expression != nil {
		if ty, ok := rule.checkArrayExpression(r.Expression, "matrix row", "jobs.<job_id>.strategy"); ok {
			if array, ok := ty.Array(); ok {
				return array.Elem()
			}
		}
		return exprtypes.AnyType()
	}

	var elements elementTypes
	for _, v := range r.Values {
		if ty, ok := rule.checkMatrixSequenceItem(v); ok {
			elements.add(ty)
		}
	}
	return elements.merged()
}

func (rule *Visitor) checkMatrixCombinationExpression(s *definition.String, section string) {
	ty, ok := rule.checkOneExpression(s, "matrix combination at element of "+section+" section", "jobs.<job_id>.strategy")
	if !ok {
		return
	}
	if array, ok := ty.Array(); ok {
		if value, known := expressionLiteral(s.Value); known {
			if values, ok := value.([]any); ok {
				// Merging object and scalar element types produces AnyType, so
				// validate each known value before using the inferred element type.
				invalid := false
				for i, value := range values {
					if _, ok := value.(map[string]any); !ok {
						rule.Errorf(scalarPosition(s), "matrix combination in %s at inserted array index %d must be an object but got %s", section, i, exprtypes.TypeOfJSONValue(value))
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
		rule.Errorf(scalarPosition(s), "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
	}
}

// ok is false when the item is an expression that inserts no values, such as
// an empty array literal.
func (rule *Visitor) checkMatrixSequenceItem(value definition.RawYAMLValue) (ty exprtypes.Type, ok bool) {
	ty = rule.checkRawYAMLValue(value)
	if scalar, ok := value.(*definition.RawYAMLString); ok && syntax.ParseAssignedExpression(scalar.Value) != nil {
		if array, ok := ty.Array(); ok {
			if literal, known := expressionLiteral(scalar.Value); known {
				if values, ok := literal.([]any); ok && len(values) == 0 {
					return exprtypes.AnyType(), false
				}
			}
			return array.Elem(), true // Template expansion inserts one level only.
		}
	}
	return ty, true
}
