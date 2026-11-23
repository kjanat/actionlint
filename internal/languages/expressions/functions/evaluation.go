package functions

import "actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"

// EvaluationContext evaluates each source argument at most once and returns the
// same observation for repeated requests.
type EvaluationContext interface {
	ArgumentCount() int
	Argument(int) exprvalues.EvaluationResult
}

// Function with a nil implementation cannot produce a constant result.
type Function struct {
	Signatures     []*FuncSignature
	implementation func(EvaluationContext) exprvalues.EvaluationResult
	inspect        func([]ParameterMatch, *CallInspection)
}

func (function Function) Evaluate(context EvaluationContext) exprvalues.EvaluationResult {
	accepted := false
	for _, signature := range function.Signatures {
		if signature.AcceptsArgumentCount(context.ArgumentCount()) {
			accepted = true
			break
		}
	}
	if !accepted {
		return exprvalues.EvaluationResult{State: exprvalues.EvaluationNotAttempted}
	}
	if function.implementation == nil {
		return exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
	}
	return function.implementation(context)
}

func knownValue(value any) exprvalues.EvaluationResult {
	return exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: value}
}

func unknownValue() exprvalues.EvaluationResult {
	return exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
}

func invalidValue() exprvalues.EvaluationResult {
	return exprvalues.EvaluationResult{State: exprvalues.EvaluationInvalid}
}
