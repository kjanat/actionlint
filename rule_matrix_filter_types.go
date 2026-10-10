package actionlint

import (
	"strconv"

	"go.yaml.in/yaml/v4"
)

func (rule *RuleMatrix) checkFilterTypes(matrix *Matrix, expressions bool) {
	for _, filters := range []*MatrixCombinations{matrix.Include, matrix.Exclude} {
		if filters == nil {
			continue
		}
		filterExpressions := expressions
		if filters.Expression != nil {
			if !expressions {
				continue
			}
			filters = knownMatrixFilters(filters.Expression)
			if filters == nil {
				continue
			}
			filterExpressions = false
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
						mismatch = mismatch || matrixFilterTypeMismatch(candidate, assign.Value, candidateExpressions, filterExpressions)
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

func knownMatrixFilters(expression *String) *MatrixCombinations {
	value, known := workflowExpressionLiteral(expression)
	if !known {
		return nil
	}
	combinations, ok := value.([]any)
	if !ok {
		return nil
	}
	for _, combination := range combinations {
		if _, ok := combination.(map[string]any); !ok {
			return nil
		}
	}
	var node yaml.Node
	if err := node.Encode(combinations); err != nil {
		return nil
	}
	var setPosition func(*yaml.Node)
	setPosition = func(node *yaml.Node) {
		node.Line, node.Column = expression.Pos.Line, expression.Pos.Col
		for _, child := range node.Content {
			setPosition(child)
		}
	}
	setPosition(&node)
	return (&parser{}).parseMatrixCombinations("matrix filters", &node)
}

func matrixLiteralValue(value any) RawYAMLValue {
	var node yaml.Node
	if node.Encode(value) != nil {
		return nil
	}
	return (&parser{}).parseRawYAMLValue(&node)
}

func matrixFilterTypeMismatch(value, filter RawYAMLValue, valueExpressions, filterExpressions bool) bool {
	return matrixFilterTypeMismatchKnown(value, filter, valueExpressions, filterExpressions, false)
}

func matrixFilterTypeMismatchKnown(value, filter RawYAMLValue, valueExpressions, filterExpressions, unknown bool) bool {
	if scalar, ok := value.(*RawYAMLString); valueExpressions && ok && ContainsExpression(scalar.Value) {
		if literal, known := workflowExpressionLiteral(&String{Value: scalar.Value}); known {
			return matrixFilterTypeMismatch(matrixLiteralValue(literal), filter, false, filterExpressions)
		}
		value, unknown = nil, true
	}
	switch f := filter.(type) {
	case *RawYAMLObject:
		for key, leaf := range f.Props {
			actual := matrixFilterValueAt(value, key)
			if matrixFilterTypeMismatchKnown(actual, leaf, valueExpressions, filterExpressions, unknown) {
				return true
			}
		}
	case *RawYAMLArray:
		for i, leaf := range f.Elems {
			actual := matrixFilterValueAt(value, strconv.Itoa(i))
			if matrixFilterTypeMismatchKnown(actual, leaf, valueExpressions, filterExpressions, unknown) {
				return true
			}
		}
	case *RawYAMLString:
		if filterExpressions && ContainsExpression(f.Value) {
			if literal, known := workflowExpressionLiteral(&String{Value: f.Value}); known {
				return matrixFilterTypeMismatchKnown(value, matrixLiteralValue(literal), valueExpressions, false, unknown)
			}
			return false
		}
		actual, ok := value.(*RawYAMLString)
		if !ok {
			if value == nil && !unknown {
				return matrixScalarKind(f.scalarValue()) != "null"
			}
			switch f.scalarValue().(type) {
			case bool, float64:
				return value == nil
			}
			return false
		}
		return matrixScalarKind(actual.scalarValue()) != matrixScalarKind(f.scalarValue())
	}
	return false
}

func matrixScalarKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	default:
		return "string"
	}
}
