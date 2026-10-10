package actionlint

import "slices"

func matrixValueContainsExpression(value RawYAMLValue) bool {
	switch value := value.(type) {
	case *RawYAMLString:
		return ContainsExpression(value.Value)
	case *RawYAMLArray:
		return slices.ContainsFunc(value.Elems, matrixValueContainsExpression)
	case *RawYAMLObject:
		for _, item := range value.Props {
			if matrixValueContainsExpression(item) {
				return true
			}
		}
	}
	return false
}

// matrixRowElements retains expression provenance after inserting one array
// level. Unknown elements leave the row open to additional runtime values.
func matrixRowElements(row *MatrixRow, expressions bool) ([]matrixFilterElement, bool) {
	values := row.Values
	if row.Expression != nil {
		literal, known := workflowExpressionLiteral(row.Expression)
		if !known {
			return nil, false
		}
		if array, ok := literal.([]any); ok && len(array) == 0 {
			return nil, true
		}
		matrix := knownLiteralMatrix(map[string]any{"value": literal}, row.Expression.Pos)
		if matrix == nil || matrix.Rows["value"] == nil {
			return nil, false
		}
		values, expressions = matrix.Rows["value"].Values, false
	}
	var elements []matrixFilterElement
	complete := true
	for _, value := range values {
		if scalar, ok := value.(*RawYAMLString); expressions && ok && ContainsExpression(scalar.Value) {
			literal, known := workflowExpressionLiteral(&String{Value: scalar.Value, Pos: scalar.Pos()})
			if known {
				inserted, ok := literal.([]any)
				if !ok {
					inserted = []any{literal}
				}
				if len(inserted) == 0 {
					continue
				}
				matrix := knownLiteralMatrix(map[string]any{"value": inserted}, scalar.Pos())
				if matrix != nil && matrix.Rows["value"] != nil {
					for _, item := range matrix.Rows["value"].Values {
						elements = append(elements, matrixFilterElement{value: item})
					}
					continue
				}
			}
			complete = false
		}
		elements = append(elements, matrixFilterElement{value, expressions})
	}
	return elements, complete
}

type matrixKnownCombination struct {
	combination *MatrixCombination
	expressions bool
}

func matrixExcludeCombinations(filters *MatrixCombinations, expressions bool) []matrixKnownCombination {
	if filters == nil {
		return nil
	}
	if filters.Expression != nil {
		if !expressions {
			return nil
		}
		return matrixExcludeCombinations(knownMatrixFilters(filters.Expression, false), false)
	}
	var combinations []matrixKnownCombination
	for _, combination := range filters.Combinations {
		if combination.Expression != nil {
			if expressions {
				combinations = append(combinations, matrixExcludeCombinations(knownMatrixFilters(combination.Expression, true), false)...)
			}
			continue
		}
		combinations = append(combinations, matrixKnownCombination{combination, expressions})
	}
	return combinations
}
