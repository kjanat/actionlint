package actionlint

import (
	"math"
	"strconv"

	"go.yaml.in/yaml/v4"
)

func (rule *RuleMatrix) checkFilterTypes(matrix *Matrix, expressions bool) {
	for _, filters := range []*MatrixCombinations{matrix.Include, matrix.Exclude} {
		rule.checkFilterSectionTypes(matrix, filters, expressions, expressions)
	}
}

func (rule *RuleMatrix) checkFilterSectionTypes(matrix *Matrix, filters *MatrixCombinations, expressions, filterExpressions bool) {
	if filters == nil {
		return
	}
	if filters.Expression != nil {
		if !filterExpressions {
			return
		}
		filters = knownMatrixFilters(filters.Expression, false)
		if filters == nil {
			return
		}
		filterExpressions = false
	}
	for _, filter := range filters.Combinations {
		if filter.Expression != nil {
			if filterExpressions {
				rule.checkFilterSectionTypes(matrix, knownMatrixFilters(filter.Expression, true), expressions, false)
			}
			continue
		}
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

func knownMatrixFilters(expression *String, insertion bool) *MatrixCombinations {
	value, known := workflowExpressionLiteral(expression)
	if !known {
		return nil
	}
	if object, ok := value.(map[string]any); insertion && ok {
		value = []any{object}
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
		lookup := newMatrixFilterLookup(value, valueExpressions)
		for key, leaf := range f.Props {
			actual, actualExpressions, uncertain := lookup.at(key)
			if uncertain {
				continue
			}
			if matrixFilterTypeMismatchKnown(actual, leaf, actualExpressions, filterExpressions, unknown) {
				return true
			}
		}
	case *RawYAMLArray:
		filters, _ := matrixFilterElements(f, filterExpressions)
		lookup := newMatrixFilterLookup(value, valueExpressions)
		for i, leaf := range filters {
			actual, actualExpressions, uncertain := lookup.at(strconv.Itoa(i))
			if uncertain {
				return false
			}
			if matrixFilterTypeMismatchKnown(actual, leaf.value, actualExpressions, leaf.expressions, unknown) {
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

type matrixFilterElement struct {
	value       RawYAMLValue
	expressions bool
}

type matrixFilterLookup struct {
	value                        RawYAMLValue
	expressions, array, complete bool
	elements                     []matrixFilterElement
}

func newMatrixFilterLookup(value RawYAMLValue, expressions bool) matrixFilterLookup {
	lookup := matrixFilterLookup{value: value, expressions: expressions, complete: true}
	if array, ok := value.(*RawYAMLArray); ok {
		lookup.array = true
		lookup.elements, lookup.complete = matrixFilterElements(array, expressions)
	}
	return lookup
}

func (lookup matrixFilterLookup) at(key string) (RawYAMLValue, bool, bool) {
	if !lookup.array {
		return matrixFilterValueAt(lookup.value, key), lookup.expressions, false
	}
	index := matrixFilterNumber(key)
	if math.IsNaN(index) || index < 0 || index > math.MaxInt32 {
		return nil, false, false
	}
	if index < float64(len(lookup.elements)) {
		item := lookup.elements[int(index)]
		return item.value, item.expressions, false
	}
	return nil, false, !lookup.complete
}

// Expression-valued sequence elements insert one array level. A dynamic element
// leaves the remaining indices unknown; retain only the preceding known prefix.
func matrixFilterElements(array *RawYAMLArray, expressions bool) ([]matrixFilterElement, bool) {
	var elements []matrixFilterElement
	for _, value := range array.Elems {
		if scalar, ok := value.(*RawYAMLString); expressions && ok && ContainsExpression(scalar.Value) {
			literal, known := workflowExpressionLiteral(&String{Value: scalar.Value})
			if !known {
				return elements, false
			}
			if inserted, ok := literal.([]any); ok {
				for _, item := range inserted {
					elements = append(elements, matrixFilterElement{value: matrixLiteralValue(item)})
				}
			} else {
				elements = append(elements, matrixFilterElement{value: matrixLiteralValue(literal)})
			}
			continue
		}
		elements = append(elements, matrixFilterElement{value, expressions})
	}
	return elements, true
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
