package expressions

import (
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	matrixcheck "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
)

func (rule *Checker) checkMatrixExpression(expr *workflowlang.String) *exprlang.ObjectType {
	ty := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	return schema.MatrixType(ty)
}

func (rule *Checker) checkMatrix(m *workflowlang.Matrix) *exprlang.ObjectType {
	if m.Expression != nil {
		return rule.checkMatrixExpression(m.Expression)
	}

	for _, filters := range []*workflowlang.MatrixCombinations{m.Include, m.Exclude} {
		if filters != nil {
			mapping.CheckSourceKey(filters.Name, filters.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
		}
	}

	// Check types of "exclude" but they are not used to guess type of matrix
	if m.Exclude != nil {
		if m.Exclude.Expression != nil {
			if ty, ok := rule.checkArrayExpression(m.Exclude.Expression, "exclude", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
				rule.checkObjectTy(ty.Elem, m.Exclude.Expression.Pos, "exclude")
			}
		} else {
			for _, combi := range m.Exclude.Combinations {
				if combi.Expression != nil {
					rule.checkMatrixCombinationExpression(combi.Expression, "exclude")
					continue
				}
				for _, a := range combi.Assigns {
					mapping.CheckSourceKey(a.Key, combi.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
					rule.checkRawYAMLValue(a.Value)
				}
			}
		}
	}

	o := exprlang.NewEmptyStrictObjectType()
	for _, insertion := range m.Insertions {
		var inserted *exprlang.ObjectType
		if insertion.Expression != nil {
			ty := rule.checkWorkflowExpression(insertion.Expression, "matrix insertion", "jobs.<job_id>.strategy", schema.Matrix)
			inserted = schema.MatrixType(ty)
		} else {
			inserted = rule.checkMatrix(insertion)
		}
		if merged, ok := o.Merge(inserted).(*exprlang.ObjectType); ok {
			o = merged
		} else {
			o.Loose()
		}
	}
	for n, r := range m.Rows {
		ty := rule.checkMatrixRow(r)
		if name, known := matrixcheck.RowName(n, r); known {
			o.Props[name] = ty
		} else {
			o.Loose()
		}
	}

	if m.Include == nil {
		return o
	}

	if m.Include.Expression != nil {
		if a, ok := rule.checkOneExpression(m.Include.Expression, "include", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			if ret, ok := o.Merge(a.Elem).(*exprlang.ObjectType); ok {
				return ret
			}
		}
		return exprlang.NewEmptyObjectType()
	}

	for _, combi := range m.Include.Combinations {
		if combi.Expression != nil {
			ty := rule.checkMatrixCombinationExpression(combi.Expression, "include")
			if ty == nil {
				continue
			}
			if merged, ok := o.Merge(ty).(*exprlang.ObjectType); ok {
				o = merged
			} else {
				o.Loose()
			}
			continue
		}

		for n, assign := range combi.Assigns {
			mapping.CheckSourceKey(assign.Key, combi.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
			key := workflowlang.InputResolver.ResolveKey(assign.Key, combi.Evaluated)
			if key.State == mapping.KnownKey {
				n = strings.ToLower(key.Name.Value)
			} else {
				o.Loose()
			}
			ty := rule.checkRawYAMLValue(assign.Value)
			if t, ok := o.Props[n]; ok {
				// When the combination exists in 'matrix' section, merge type with existing one
				ty = t.Merge(ty)
			}
			if key.State == mapping.KnownKey {
				o.Props[n] = ty
			}
		}
	}

	return o
}

func (rule *Checker) checkMatrixRow(r *workflowlang.MatrixRow) exprlang.Type {
	mapping.CheckSourceKey(r.Name, r.Evaluated, func(source *workflowlang.String) {
		rule.checkString(source, "jobs.<job_id>.strategy")
	})
	if r.Expression != nil {
		if a, ok := rule.checkArrayExpression(r.Expression, "matrix row", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			return a.Elem
		}
		return exprlang.AnyType{}
	}

	var ty exprlang.Type
	for _, v := range r.Values {
		t := rule.checkMatrixSequenceItem(v)
		if t == nil {
			continue
		}
		if ty == nil {
			ty = t
		} else {
			ty = ty.Merge(t)
		}
	}

	if ty == nil {
		return exprlang.AnyType{} // No element
	}

	return ty
}

func (rule *Checker) checkMatrixCombinationExpression(s *workflowlang.String, section string) exprlang.Type {
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
					return nil
				}
			}
		}
		ty = array.Elem
	}
	switch ty.(type) {
	case nil, exprlang.AnyType, *exprlang.ObjectType:
		return ty
	default:
		rule.Errorf(s.Pos, "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
		return nil
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
