package functions

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func evaluateFormat(context EvaluationContext) values.EvaluationResult {
	first := context.Argument(0)
	if first.State != values.EvaluationKnown {
		return first
	}
	template, known := values.FunctionString(first.Value)
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
		if argument.State != values.EvaluationKnown {
			return argument
		}
		if part.Specifier != "" {
			return invalidValue()
		}
		value, known := values.FunctionString(argument.Value)
		if !known {
			return unknownValue()
		}
		output.WriteString(value)
	}
	return knownValue(output.String())
}
