package mapping

type Matrix[R any, F, S comparable] struct {
	Rows             map[string]R
	Include, Exclude F
	Expression       S
	Insertions       []*Matrix[R, F, S]
}

func ExpandMatrix[R any, F, S comparable](matrix *Matrix[R, F, S], evaluate func(S) *Matrix[R, F, S], resolveName func(string, R) (string, bool)) (*Matrix[R, F, S], bool) {
	var zeroSource S
	var zeroFilter F
	if matrix.Expression != zeroSource {
		result := evaluate(matrix.Expression)
		if result == nil {
			return nil, false
		}
		return ExpandMatrix(result, evaluate, resolveName)
	}
	result := *matrix
	result.Rows = map[string]R{}
	result.Insertions = nil
	complete := true
	for name, row := range matrix.Rows {
		resolved, known := resolveName(name, row)
		complete = complete && known
		if !known {
			continue
		}
		if _, duplicate := result.Rows[resolved]; duplicate {
			return nil, false
		}
		result.Rows[resolved] = row
	}
	for _, insertion := range matrix.Insertions {
		inserted, known := ExpandMatrix(insertion, evaluate, resolveName)
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
		if inserted.Include != zeroFilter {
			if result.Include != zeroFilter {
				return nil, false
			}
			result.Include = inserted.Include
		}
		if inserted.Exclude != zeroFilter {
			if result.Exclude != zeroFilter {
				return nil, false
			}
			result.Exclude = inserted.Exclude
		}
	}
	return &result, complete
}

func ExpandCombinations[C any, S comparable](expression S, items []C, itemExpression func(C) S, evaluate func(S, bool) ([]C, bool)) ([]C, bool) {
	var zero S
	if expression != zero {
		return evaluate(expression, false)
	}
	var result []C
	for _, item := range items {
		if source := itemExpression(item); source != zero {
			inserted, known := evaluate(source, true)
			if !known {
				return nil, false
			}
			result = append(result, inserted...)
		} else {
			result = append(result, item)
		}
	}
	return result, true
}
