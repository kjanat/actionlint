package matrix

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
)

// Evaluate leaves other contexts and unsupported operations dynamic, and
// missing matrix properties take the runner's null value.
func (c Combination) Evaluate(text string) value.Value {
	expression := syntax.ParseAssignedExpression(text)
	if expression == nil {
		return value.Template(text, value.Origin{})
	}
	result, known := evaluate.ExpressionValue(expression, c.LookupExpressionVariable)
	if !known {
		return value.DynamicValue(text, value.KindUnknown, value.Origin{FromExpression: true})
	}
	return value.FromJSON(result, value.Origin{FromExpression: true})
}

// LookupExpressionVariable makes the object unknown for an unresolved member and keeps the member.
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
