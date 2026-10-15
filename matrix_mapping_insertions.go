package actionlint

import "maps"

// Resolve known insertions without modifying the parsed tree or reinterpreting
// evaluated strings as expression source. Opaque or overlapping mappings leave
// combination-dependent diagnostics deferred to runtime.
func expandedMatrixInsertions(matrix *Matrix) (*Matrix, bool) {
	if matrix.Expression != nil {
		value, known := workflowExpressionLiteral(matrix.Expression)
		if !known {
			return nil, false
		}
		result := knownLiteralMatrix(value, matrix.Expression.Pos)
		return result, result != nil
	}
	result := *matrix
	result.Rows = maps.Clone(matrix.Rows)
	if result.Rows == nil {
		result.Rows = map[string]*MatrixRow{}
	}
	result.Insertions = nil
	complete := true
	for _, insertion := range matrix.Insertions {
		inserted, known := expandedMatrixInsertions(insertion)
		complete = complete && known
		if inserted == nil {
			continue
		}
		for name, row := range inserted.Rows {
			if _, duplicate := result.Rows[name]; duplicate {
				return nil, false
			}
			result.Rows[name] = row
		}
		if inserted.Include != nil {
			if result.Include != nil {
				return nil, false
			}
			result.Include = inserted.Include
		}
		if inserted.Exclude != nil {
			if result.Exclude != nil {
				return nil, false
			}
			result.Exclude = inserted.Exclude
		}
	}
	return &result, complete
}

func expandedMatrixCombinations(filters *MatrixCombinations) (*MatrixCombinations, bool) {
	if filters == nil {
		return nil, true
	}
	if filters.Expression != nil {
		result := knownMatrixFilters(filters.Expression, false)
		if result == nil {
			return nil, false
		}
		for _, combination := range result.Combinations {
			combination.evaluated = true
		}
		return result, true
	}
	result := &MatrixCombinations{}
	for _, combination := range filters.Combinations {
		if combination.Expression == nil {
			result.Combinations = append(result.Combinations, combination)
			continue
		}
		inserted := knownMatrixFilters(combination.Expression, true)
		if inserted == nil {
			return nil, false
		}
		for _, item := range inserted.Combinations {
			item.evaluated = true
		}
		result.Combinations = append(result.Combinations, inserted.Combinations...)
	}
	return result, true
}
