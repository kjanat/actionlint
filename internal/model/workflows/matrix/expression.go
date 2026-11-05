package matrix

import (
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

// Evaluate resolves an assigned expression against this exact matrix row.
// Other contexts and unsupported operations remain dynamic. Missing properties
// of the complete matrix object have the runner's null value.
func (c Combination) Evaluate(text string) value.Value {
	expression := exprsyntax.ParseAssignedExpression(text)
	if expression == nil {
		return value.Template(text, value.Origin{})
	}
	result, known := expreval.ExpressionValue(expression, c.LookupExpressionVariable)
	if !known {
		return value.DynamicValue(text, value.KindUnknown, value.Origin{FromExpression: true})
	}
	return value.FromJSON(result, value.Origin{FromExpression: true})
}

// LookupExpressionVariable exposes a complete matrix row to the shared evaluator.
// An unresolved member makes the object unknown rather than silently omitting it.
func (c Combination) LookupExpressionVariable(name string) (any, bool) {
	if name != "matrix" {
		return nil, false
	}
	matrix := make(map[string]any, len(c.values))
	for name, item := range c.values {
		literal, known := expressionValue(item)
		if !known {
			return nil, false
		}
		matrix[name] = literal
	}
	return matrix, true
}

func expressionValue(item value.Value) (any, bool) {
	if !item.Known() {
		return nil, false
	}
	switch item.Kind() {
	case value.Null:
		return nil, true
	case value.Bool:
		return item.Bool()
	case value.Number:
		return item.Number()
	case value.String:
		return item.Text()
	case value.Array:
		result := make([]any, 0, item.Len())
		for child := range item.Items() {
			literal, known := expressionValue(child)
			if !known {
				return nil, false
			}
			result = append(result, literal)
		}
		return result, true
	case value.Object:
		result := make(map[string]any, item.Len())
		for name, child := range item.Props() {
			literal, known := expressionValue(child)
			if !known {
				return nil, false
			}
			result[name] = literal
		}
		return result, true
	case value.KindUnknown:
		return nil, false
	default:
		return nil, false
	}
}
