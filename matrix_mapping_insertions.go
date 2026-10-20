package actionlint

import "actionlint.kjanat.dev/internal/mapping"

type matrixInsertionView = mapping.Matrix[*MatrixRow, *MatrixCombinations, *String]

func matrixMappingView(matrix *Matrix) *matrixInsertionView {
	if matrix == nil {
		return nil
	}
	result := &matrixInsertionView{Rows: matrix.Rows, Include: matrix.Include, Exclude: matrix.Exclude, Expression: matrix.Expression}
	for _, insertion := range matrix.Insertions {
		result.Insertions = append(result.Insertions, matrixMappingView(insertion))
	}
	return result
}

func expandedMatrixInsertions(matrix *Matrix) (*Matrix, bool) {
	resolved, complete := mapping.ExpandMatrix(matrixMappingView(matrix), func(source *String) *matrixInsertionView {
		value, known := workflowExpressionLiteral(source)
		if !known {
			return nil
		}
		return matrixMappingView(knownLiteralMatrix(value, source.Pos))
	})
	if resolved == nil {
		return nil, false
	}
	pos := matrix.Pos
	if matrix.Expression != nil {
		pos = matrix.Expression.Pos
	}
	return &Matrix{Rows: resolved.Rows, Include: resolved.Include, Exclude: resolved.Exclude, Pos: pos}, complete
}

func expandedMatrixCombinations(filters *MatrixCombinations) (*MatrixCombinations, bool) {
	if filters == nil {
		return nil, true
	}
	items, complete := mapping.ExpandCombinations(filters.Expression, filters.Combinations,
		func(item *MatrixCombination) *String { return item.Expression },
		func(source *String, splice bool) ([]*MatrixCombination, bool) {
			result := knownMatrixFilters(source, splice)
			if result == nil {
				return nil, false
			}
			for _, item := range result.Combinations {
				item.evaluated = true
			}
			return result.Combinations, true
		})
	if !complete {
		return nil, false
	}
	return &MatrixCombinations{Combinations: items}, true
}
