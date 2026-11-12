package functions

import (
	"strings"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func evaluateJoin(context EvaluationContext) exprvalues.EvaluationResult {
	first := context.Argument(0)
	if first.State != exprvalues.EvaluationKnown {
		return first
	}
	items, array := first.Value.([]any)
	if !array {
		if _, object := first.Value.(map[string]any); object {
			return knownValue("")
		}
		value, known := exprvalues.JoinString(first.Value)
		if !known {
			return unknownValue()
		}
		return knownValue(value)
	}
	separator := ","
	if context.ArgumentCount() == 2 && len(items) > 1 {
		argument := context.Argument(1)
		if argument.State != exprvalues.EvaluationKnown {
			return argument
		}
		switch argument.Value.(type) {
		case []any, map[string]any:
			// Composite separators do not replace the default comma.
		default:
			var known bool
			separator, known = exprvalues.JoinString(argument.Value)
			if !known {
				return unknownValue()
			}
		}
	}
	parts := make([]string, len(items))
	for index, item := range items {
		value, known := exprvalues.JoinString(item)
		if !known {
			return unknownValue()
		}
		parts[index] = value
	}
	return knownValue(strings.Join(parts, separator))
}
