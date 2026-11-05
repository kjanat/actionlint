package functions

import (
	"strings"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func evaluateFormat(context EvaluationContext) exprvalues.EvaluationResult {
	first := context.Argument(0)
	if first.State != exprvalues.EvaluationKnown {
		return first
	}
	template, known := exprvalues.FunctionString(first.Value)
	if !known {
		return unknownValue()
	}
	plan := PlanFormat(template, context.ArgumentCount()-1)
	if !plan.Valid() {
		return invalidValue()
	}
	var output strings.Builder
	for part := range plan.Parts() {
		if part.Index < 0 {
			output.WriteString(part.Text)
			continue
		}
		argument := context.Argument(part.Index + 1)
		if argument.State != exprvalues.EvaluationKnown {
			return argument
		}
		if part.Specifier != "" {
			return invalidValue()
		}
		value, known := exprvalues.FunctionString(argument.Value)
		if !known {
			return unknownValue()
		}
		output.WriteString(value)
	}
	return knownValue(output.String())
}
