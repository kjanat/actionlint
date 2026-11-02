package matrix

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// Evaluate resolves an assigned expression against this exact matrix row.
// Other contexts and unsupported operations remain dynamic. Missing properties
// of the complete matrix object have the runner's null value.
func (c Combination) Evaluate(text string) value.Value {
	expression := exprlang.ParseAssignedExpression(text)
	if expression == nil {
		return value.Template(text, value.Origin{})
	}
	matrix := make(map[string]any, len(c.values))
	for name, item := range c.values {
		literal, known := expressionValue(item)
		if !known {
			return value.DynamicValue(text, value.KindUnknown, value.Origin{FromExpression: true})
		}
		matrix[name] = literal
	}
	result, known := exprlang.ExpressionValue(expression, func(name string) (any, bool) {
		return matrix, name == "matrix"
	})
	if !known {
		return value.DynamicValue(text, value.KindUnknown, value.Origin{FromExpression: true})
	}
	return value.FromJSON(result, value.Origin{FromExpression: true})
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
