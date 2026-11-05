package functions

import (
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// IsStringPredicate reports whether a built-in tests strings and returns a boolean.
func IsStringPredicate(name string) bool {
	switch strings.ToLower(name) {
	case "contains", "startswith", "endswith":
		return true
	default:
		return false
	}
}

func evaluateContains(context EvaluationContext) exprvalues.EvaluationResult {
	return evaluatePredicate(context, "contains")
}

func evaluateStartsWith(context EvaluationContext) exprvalues.EvaluationResult {
	return evaluatePredicate(context, "startswith")
}

func evaluateEndsWith(context EvaluationContext) exprvalues.EvaluationResult {
	return evaluatePredicate(context, "endswith")
}

func evaluatePredicate(context EvaluationContext, operation string) exprvalues.EvaluationResult {
	first := context.Argument(0)
	if first.State != exprvalues.EvaluationKnown {
		return first
	}
	if items, array := first.Value.([]any); array && operation == "contains" {
		if len(items) == 0 {
			return knownValue(false)
		}
		second := context.Argument(1)
		if second.State != exprvalues.EvaluationKnown {
			return second
		}
		known := true
		for _, item := range items {
			equal, comparisonKnown := exprvalues.CompareConditionValues(exprsyntax.CompareOpNodeKindEq, item, second.Value)
			if comparisonKnown && equal {
				return knownValue(true)
			}
			known = known && comparisonKnown
		}
		if !known {
			return unknownValue()
		}
		return knownValue(false)
	}
	switch first.Value.(type) {
	case []any, map[string]any:
		return knownValue(false)
	}
	second := context.Argument(1)
	if second.State != exprvalues.EvaluationKnown {
		return second
	}
	switch second.Value.(type) {
	case []any, map[string]any:
		return knownValue(false)
	}
	left, leftKnown := exprvalues.PredicateString(first.Value)
	right, rightKnown := exprvalues.PredicateString(second.Value)
	if !leftKnown || !rightKnown {
		return unknownValue()
	}
	left, right = runnerjson.OrdinalIgnoreCaseKey(left), runnerjson.OrdinalIgnoreCaseKey(right)
	switch operation {
	case "contains":
		return knownValue(strings.Contains(left, right))
	case "startswith":
		return knownValue(strings.HasPrefix(left, right))
	default:
		return knownValue(strings.HasSuffix(left, right))
	}
}
