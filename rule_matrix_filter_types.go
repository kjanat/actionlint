package actionlint

import "go.yaml.in/yaml/v4"

func (rule *RuleMatrix) checkFilterTypes(matrix *Matrix, expressions bool) {
	for _, filters := range []*MatrixCombinations{matrix.Include, matrix.Exclude} {
		if filters == nil {
			continue
		}
		for _, filter := range filters.Combinations {
			for key, assign := range filter.Assigns {
				row := matrix.Rows[key]
				if row == nil {
					continue // Include may add properties which are not axes.
				}
				values := row.Values
				valueExpressions := expressions
				if row.Expression != nil {
					values = []RawYAMLValue{&RawYAMLString{Value: row.Expression.Value}}
					if value, known := workflowExpressionLiteral(row.Expression); known {
						if array, ok := value.([]any); ok {
							valueExpressions = false
							values = nil
							for _, item := range array {
								values = append(values, matrixLiteralValue(item))
							}
						}
					}
				}
				for _, value := range values {
					candidates := []RawYAMLValue{value}
					candidateExpressions := valueExpressions
					if scalar, ok := value.(*RawYAMLString); ok && valueExpressions {
						if literal, known := workflowExpressionLiteral(&String{Value: scalar.Value}); known {
							candidateExpressions = false
							if array, ok := literal.([]any); ok {
								candidates = nil
								for _, item := range array {
									candidates = append(candidates, matrixLiteralValue(item))
								}
							} else {
								candidates = []RawYAMLValue{matrixLiteralValue(literal)}
							}
						}
					}
					mismatch := false
					for _, candidate := range candidates {
						mismatch = mismatch || matrixFilterTypeMismatch(candidate, assign.Value, candidateExpressions, expressions)
					}
					if mismatch {
						rule.Errorf(assign.Value.Pos(), "matrix filter for %q compares different or unknown scalar types using Actions loose equality; validate axis types before filtering (policy: mixed-type-matrix-filters)", key)
						break
					}
				}
			}
		}
	}
}

func matrixLiteralValue(value any) RawYAMLValue {
	var node yaml.Node
	if node.Encode(value) != nil {
		return nil
	}
	return (&parser{}).parseRawYAMLValue(&node)
}

func matrixFilterTypeMismatch(value, filter RawYAMLValue, valueExpressions, filterExpressions bool) bool {
	if scalar, ok := value.(*RawYAMLString); valueExpressions && ok && ContainsExpression(scalar.Value) {
		if literal, known := workflowExpressionLiteral(&String{Value: scalar.Value}); known {
			return matrixFilterTypeMismatch(matrixLiteralValue(literal), filter, false, filterExpressions)
		}
		value = nil // Unknown dynamic values matter for numeric and boolean filters.
	}
	switch f := filter.(type) {
	case *RawYAMLObject:
		object, _ := value.(*RawYAMLObject)
		for key, leaf := range f.Props {
			var actual RawYAMLValue
			if object != nil {
				actual = object.Props[key]
			}
			if matrixFilterTypeMismatch(actual, leaf, valueExpressions, filterExpressions) {
				return true
			}
		}
	case *RawYAMLArray:
		array, _ := value.(*RawYAMLArray)
		for i, leaf := range f.Elems {
			var actual RawYAMLValue
			if array != nil && i < len(array.Elems) {
				actual = array.Elems[i]
			}
			if matrixFilterTypeMismatch(actual, leaf, valueExpressions, filterExpressions) {
				return true
			}
		}
	case *RawYAMLString:
		if filterExpressions && ContainsExpression(f.Value) {
			return false
		}
		actual, ok := value.(*RawYAMLString)
		if !ok {
			switch f.scalarValue().(type) {
			case bool, float64:
				return value == nil
			}
			return false
		}
		_, actualString := actual.scalarValue().(string)
		_, filterString := f.scalarValue().(string)
		return actualString != filterString
	}
	return false
}
