package matrix

import (
	"slices"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func matrixValueContainsExpression(value workflowlang.RawYAMLValue) bool {
	switch value := value.(type) {
	case *workflowlang.RawYAMLString:
		return exprlang.ContainsExpression(value.Value)
	case *workflowlang.RawYAMLArray:
		return slices.ContainsFunc(value.Elems, matrixValueContainsExpression)
	case *workflowlang.RawYAMLObject:
		for _, item := range value.Props {
			if matrixValueContainsExpression(item) {
				return true
			}
		}
	}
	return false
}

// RowElements retains expression provenance after inserting one array
// level. Unknown elements leave the row open to additional runtime values.
func RowElements(row *workflowlang.MatrixRow, expressions bool) ([]Element, bool) {
	expressions = expressions && !row.Evaluated
	values := row.Values
	if row.Expression != nil {
		literal, known := workflowlang.WorkflowExpressionLiteral(row.Expression)
		if !known {
			return nil, false
		}
		if array, ok := literal.([]any); ok && len(array) == 0 {
			return nil, true
		}
		matrix := knownLiteral(map[string]any{"value": literal}, row.Expression.Pos)
		if matrix == nil || matrix.Rows["value"] == nil {
			return nil, false
		}
		values, expressions = matrix.Rows["value"].Values, false
	}
	var elements []Element
	complete := true
	for _, value := range values {
		if scalar, ok := value.(*workflowlang.RawYAMLString); expressions && ok && exprlang.ContainsExpression(scalar.Value) {
			literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value, Pos: scalar.Pos()})
			if known {
				inserted, ok := literal.([]any)
				if !ok {
					inserted = []any{literal}
				}
				if len(inserted) == 0 {
					continue
				}
				matrix := knownLiteral(map[string]any{"value": inserted}, scalar.Pos())
				if matrix != nil && matrix.Rows["value"] != nil {
					for _, item := range matrix.Rows["value"].Values {
						elements = append(elements, Element{Value: item})
					}
					continue
				}
			}
			complete = false
		}
		elements = append(elements, Element{value, expressions})
	}
	return elements, complete
}

// Combination retains expression provenance while expanding filter entries.
type Combination struct {
	Combination *workflowlang.MatrixCombination
	Expressions bool
}

// Combinations expands known entries and omits entries whose values remain dynamic.
func Combinations(filters *workflowlang.MatrixCombinations, expressions bool) []Combination {
	if filters == nil {
		return nil
	}
	if filters.Expression != nil {
		if !expressions {
			return nil
		}
		return Combinations(knownMatrixFilters(filters.Expression, false), false)
	}
	var combinations []Combination
	for _, combination := range filters.Combinations {
		if combination.Expression != nil {
			if expressions {
				combinations = append(combinations, Combinations(knownMatrixFilters(combination.Expression, true), false)...)
			}
			continue
		}
		combinations = append(combinations, Combination{combination, expressions && !combination.Evaluated})
	}
	return combinations
}
