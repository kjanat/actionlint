package functions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func inspectCase(arguments []ParameterMatch, inspection *CallInspection) {
	for index := 0; index < len(arguments)-1; index += 2 {
		match := arguments[index]
		argument := match.Argument
		if match.Accepted {
			continue
		}
		actual := argument.Source
		if argument.State == ArgumentKnown {
			actual = types.TypeOfJSONValue(argument.Value)
		}
		inspection.addProblem(InvalidCasePredicate{Index: index, Actual: actual}, argument.Demand)
	}
}

func evaluateCase(context EvaluationContext) values.EvaluationResult {
	for index := 0; index < context.ArgumentCount()-1; index += 2 {
		predicate := context.Argument(index)
		if predicate.State != values.EvaluationKnown {
			return predicate
		}
		value, boolean := predicate.Value.(bool)
		if !boolean {
			return invalidValue()
		}
		if value {
			return context.Argument(index + 1)
		}
	}
	return context.Argument(context.ArgumentCount() - 1)
}
