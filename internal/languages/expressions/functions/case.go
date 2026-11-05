package functions

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func inspectCase(arguments []ParameterMatch, inspection *CallInspection) {
	for index := 0; index < len(arguments)-1; index += 2 {
		match := arguments[index]
		argument := match.Argument
		if match.Accepted || argument.Demand == DemandSkipped {
			continue
		}
		actual := argument.Source
		if argument.State == ArgumentKnown {
			actual = exprtypes.TypeOfJSONValue(argument.Value)
		}
		inspection.Problems = append(inspection.Problems, InvalidCasePredicate{Index: index, Actual: actual, Demand: argument.Demand})
	}
}

func evaluateCase(context EvaluationContext) exprvalues.EvaluationResult {
	for index := 0; index < context.ArgumentCount()-1; index += 2 {
		predicate := context.Argument(index)
		if predicate.State != exprvalues.EvaluationKnown {
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
