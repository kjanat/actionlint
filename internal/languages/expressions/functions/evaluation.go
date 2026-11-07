package functions

import exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

// EvaluationContext supplies argument values to a function implementation.
// Argument evaluates a source argument at most once. Repeated requests return the
// same observation. Implementations request only the arguments their operation needs.
type EvaluationContext interface {
	ArgumentCount() int
	Argument(int) exprvalues.EvaluationResult
}

// Function stores overload contracts and one implementation for their operation.
// A nil implementation means this constant evaluator cannot produce its result.
type Function struct {
	Signatures     []*FuncSignature
	implementation func(EvaluationContext) exprvalues.EvaluationResult
	inspect        func([]ParameterMatch, *CallInspection)
}

// Evaluate validates argument count before requesting values from the context.
// Unimplemented operations produce an unknown result without requesting arguments.
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
