package matrix

import (
	"math"
	"strconv"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func (rule *checker) checkFilterTypes(matrix *workflowlang.Matrix, expressions bool) {
	for _, filters := range []*workflowlang.MatrixCombinations{matrix.Include, matrix.Exclude} {
		rule.checkFilterSectionTypes(matrix, filters, expressions, expressions)
	}
}

func (rule *checker) checkFilterSectionTypes(matrix *workflowlang.Matrix, filters *workflowlang.MatrixCombinations, expressions, filterExpressions bool) {
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
			valueExpressions := expressions && !row.Evaluated
			if row.Expression != nil {
				values = []workflowlang.RawYAMLValue{&workflowlang.RawYAMLString{Value: row.Expression.Value}}
				if value, known := workflowlang.WorkflowExpressionLiteral(row.Expression); known {
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
				candidates := []workflowlang.RawYAMLValue{value}
				candidateExpressions := valueExpressions
				if scalar, ok := value.(*workflowlang.RawYAMLString); ok && valueExpressions {
					if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value}); known {
						candidateExpressions = false
						if array, ok := literal.([]any); ok {
							candidates = nil
							for _, item := range array {
								candidates = append(candidates, matrixLiteralValue(item))
							}
						} else {
							candidates = []workflowlang.RawYAMLValue{matrixLiteralValue(literal)}
						}
					}
				}
				mismatch := false
				for _, candidate := range candidates {
					mismatch = mismatch || matrixFilterTypeMismatch(candidate, assign.Value, candidateExpressions, filterExpressions && !filter.Evaluated)
				}
				if mismatch {
					rule.findings = append(rule.findings, findingAt(assign.Value.Pos(), "mixed-type-matrix-filters", "matrix filter for %q compares different or unknown scalar types using Actions loose equality; validate axis types before filtering", key))
					break
				}
			}
		}
	}
}

func knownMatrixFilters(expression *workflowlang.String, insertion bool) *workflowlang.MatrixCombinations {
	value, known := workflowlang.WorkflowExpressionLiteral(expression)
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
	return (&workflowlang.Parser{}).ParseMatrixCombinations("matrix filters", &node)
}

func matrixLiteralValue(value any) workflowlang.RawYAMLValue {
	var node yaml.Node
	if node.Encode(value) != nil {
		return nil
	}
	return (&workflowlang.Parser{}).ParseRawYAMLValue(&node)
}

func matrixFilterTypeMismatch(value, filter workflowlang.RawYAMLValue, valueExpressions, filterExpressions bool) bool {
	return matrixFilterTypeMismatchKnown(value, filter, valueExpressions, filterExpressions, false)
}

func matrixFilterTypeMismatchKnown(value, filter workflowlang.RawYAMLValue, valueExpressions, filterExpressions, unknown bool) bool {
	if scalar, ok := value.(*workflowlang.RawYAMLString); valueExpressions && ok && exprlang.ContainsExpression(scalar.Value) {
		if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value}); known {
			return matrixFilterTypeMismatch(matrixLiteralValue(literal), filter, false, filterExpressions)
		}
		value, unknown = nil, true
	}
	switch f := filter.(type) {
	case *workflowlang.RawYAMLObject:
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
	case *workflowlang.RawYAMLArray:
		filters, _ := matrixFilterElements(f, filterExpressions)
		lookup := newMatrixFilterLookup(value, valueExpressions)
		for i, leaf := range filters {
			actual, actualExpressions, uncertain := lookup.at(strconv.Itoa(i))
			if uncertain {
				return false
			}
			if matrixFilterTypeMismatchKnown(actual, leaf.Value, actualExpressions, leaf.Expressions, unknown) {
				return true
			}
		}
	case *workflowlang.RawYAMLString:
		if filterExpressions && exprlang.ContainsExpression(f.Value) {
			if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: f.Value}); known {
				return matrixFilterTypeMismatchKnown(value, matrixLiteralValue(literal), valueExpressions, false, unknown)
			}
			return false
		}
		actual, ok := value.(*workflowlang.RawYAMLString)
		if !ok {
			if value == nil && !unknown {
				return matrixScalarKind(f.ScalarValue()) != "null"
			}
			switch f.ScalarValue().(type) {
			case bool, float64:
				return value == nil
			}
			return false
		}
		return matrixScalarKind(actual.ScalarValue()) != matrixScalarKind(f.ScalarValue())
	}
	return false
}

// Element pairs a matrix value with its expression-evaluation mode.
type Element struct {
	Value       workflowlang.RawYAMLValue
	Expressions bool
}

type matrixFilterLookup struct {
	Value                        workflowlang.RawYAMLValue
	Expressions, array, complete bool
	elements                     []Element
}

func newMatrixFilterLookup(value workflowlang.RawYAMLValue, expressions bool) matrixFilterLookup {
	lookup := matrixFilterLookup{Value: value, Expressions: expressions, complete: true}
	if array, ok := value.(*workflowlang.RawYAMLArray); ok {
		lookup.array = true
		lookup.elements, lookup.complete = matrixFilterElements(array, expressions)
	}
	return lookup
}

func (lookup matrixFilterLookup) at(key string) (workflowlang.RawYAMLValue, bool, bool) {
	if !lookup.array {
		return matrixFilterValueAt(lookup.Value, key), lookup.Expressions, false
	}
	index := exprlang.ToNumber(key)
	if math.IsNaN(index) || index < 0 || index > math.MaxInt32 {
		return nil, false, false
	}
	if index < float64(len(lookup.elements)) {
		item := lookup.elements[int(index)]
		return item.Value, item.Expressions, false
	}
	return nil, false, !lookup.complete
}

// matrixFilterElements expands one array level, stopping at the first dynamic
// insertion because its length determines the indexes of subsequent elements.
func matrixFilterElements(array *workflowlang.RawYAMLArray, expressions bool) ([]Element, bool) {
	var elements []Element
	for _, value := range array.Elems {
		if scalar, ok := value.(*workflowlang.RawYAMLString); expressions && ok && exprlang.ContainsExpression(scalar.Value) {
			literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value})
			if !known {
				return elements, false
			}
			if inserted, ok := literal.([]any); ok {
				for _, item := range inserted {
					elements = append(elements, Element{Value: matrixLiteralValue(item)})
				}
			} else {
				elements = append(elements, Element{Value: matrixLiteralValue(literal)})
			}
			continue
		}
		elements = append(elements, Element{value, expressions})
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
