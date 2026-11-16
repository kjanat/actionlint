package functions

import "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

// EvaluationContext supplies argument values to a function implementation.
// Argument evaluates a source argument at most once. Repeated requests return the
// same observation. Implementations request only the arguments their operation needs.
type EvaluationContext interface {
	ArgumentCount() int
	Argument(int) values.EvaluationResult
}

// Function stores overload contracts and one implementation for their operation.
// A nil implementation means this constant evaluator cannot produce its result.
type Function struct {
	Signatures     []*FuncSignature
	implementation func(EvaluationContext) values.EvaluationResult
	inspect        func([]ParameterMatch, *CallInspection)
}

// Evaluate validates argument count before requesting values from the context.
// Unimplemented operations produce an unknown result without requesting arguments.
func (function Function) Evaluate(context EvaluationContext) values.EvaluationResult {
	accepted := false
	for _, signature := range function.Signatures {
		if signature.AcceptsArgumentCount(context.ArgumentCount()) {
			accepted = true
			break
		}
	}
	if !accepted {
		return values.EvaluationResult{State: values.EvaluationNotAttempted}
	}
	if function.implementation == nil {
		return values.EvaluationResult{State: values.EvaluationUnknown}
	}
	return function.implementation(context)
}

func knownValue(value any) values.EvaluationResult {
	return values.EvaluationResult{State: values.EvaluationKnown, Value: value}
}

func unknownValue() values.EvaluationResult {
	return values.EvaluationResult{State: values.EvaluationUnknown}
}

func invalidValue() values.EvaluationResult {
	return values.EvaluationResult{State: values.EvaluationInvalid}
}
