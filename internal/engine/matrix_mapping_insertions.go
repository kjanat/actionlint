package engine

import (
	"strings"

	"actionlint.kjanat.dev/internal/mapping"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type matrixInsertionView = mapping.Matrix[*workflowlang.MatrixRow, *workflowlang.MatrixCombinations, *workflowlang.String]

func matrixMappingView(matrix *workflowlang.Matrix) *matrixInsertionView {
	if matrix == nil {
		return nil
	}
	result := &matrixInsertionView{Rows: matrix.Rows, Include: matrix.Include, Exclude: matrix.Exclude, Expression: matrix.Expression}
	for _, insertion := range matrix.Insertions {
		result.Insertions = append(result.Insertions, matrixMappingView(insertion))
	}
	return result
}

func expandedMatrixInsertions(matrix *workflowlang.Matrix) (*workflowlang.Matrix, bool) {
	resolved, complete := mapping.ExpandMatrix(matrixMappingView(matrix), func(source *workflowlang.String) *matrixInsertionView {
		value, known := workflowlang.WorkflowExpressionLiteral(source)
		if !known {
			return nil
		}
		return matrixMappingView(knownLiteralMatrix(value, source.Pos))
	}, resolvedMatrixRowName)
	if resolved == nil {
		return nil, false
	}
	pos := matrix.Pos
	if matrix.Expression != nil {
		pos = matrix.Expression.Pos
	}
	return &workflowlang.Matrix{Rows: resolved.Rows, Include: resolved.Include, Exclude: resolved.Exclude, Pos: pos}, complete
}

func expandedMatrixCombinations(filters *workflowlang.MatrixCombinations) (*workflowlang.MatrixCombinations, bool) {
	if filters == nil {
		return nil, true
	}
	items, complete := mapping.ExpandCombinations(filters.Expression, filters.Combinations,
		func(item *workflowlang.MatrixCombination) *workflowlang.String { return item.Expression },
		func(source *workflowlang.String, splice bool) ([]*workflowlang.MatrixCombination, bool) {
			result := knownMatrixFilters(source, splice)
			if result == nil {
				return nil, false
			}
			for _, item := range result.Combinations {
				item.Evaluated = true
			}
			return result.Combinations, true
		})
	if !complete {
		return nil, false
	}
	return &workflowlang.MatrixCombinations{Combinations: items}, true
}

func resolvedMatrixRowName(fallback string, row *workflowlang.MatrixRow) (string, bool) {
	if row.Name == nil {
		return fallback, true
	}
	key := workflowlang.InputResolver.ResolveKey(row.Name, row.Evaluated)
	if key.State != mapping.KnownKey {
		return "", false
	}
	return strings.ToLower(key.Name.Value), true
}
