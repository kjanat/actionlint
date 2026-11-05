package functions

import exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

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
