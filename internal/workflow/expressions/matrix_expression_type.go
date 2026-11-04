package expressions

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

type expressionRefinement struct {
	ty                 exprlang.Type
	state              value.State
	templateInvalid    exprlang.Type
	hasTemplateInvalid bool
}

// expressionType keeps finite matrix-dependent expressions precise without
// choosing one row's value for another. Unknown or oversized matrices retain
// the ordinary semantic checker's type.
func (rule *Checker) expressionType(text string, evaluation exprlang.EvaluationResult) expressionRefinement {
	switch evaluation.State {
	case exprlang.EvaluationKnown:
		return expressionRefinement{ty: exprlang.TypeOfJSONValue(evaluation.Value), state: value.Evaluated}
	case exprlang.EvaluationInvalid, exprlang.EvaluationNotAttempted:
		return expressionRefinement{state: value.Invalid}
	case exprlang.EvaluationUnknown:
	}
	if !rule.scriptJob.Present() {
		return expressionRefinement{state: value.Dynamic}
	}
	combinations, complete := rule.matrixResolution(rule.scriptJob.Strategy()).Combinations(256)
	if !complete || len(combinations) == 0 {
		return expressionRefinement{state: value.Dynamic}
	}
	result := expressionRefinement{state: value.Evaluated}
	first := true
	for _, combination := range combinations {
		resolved := combination.Evaluate(text)
		if !resolved.Known() {
			if resolved.State() == value.Invalid {
				result.state = value.Invalid
			} else if result.state != value.Invalid {
				result.state = value.Dynamic
			}
			continue
		}
		inferred := matrixValueType(resolved, exprlang.AnyType())
		switch inferred.Kind() {
		case exprlang.KindObject, exprlang.KindArray:
			result.templateInvalid, result.hasTemplateInvalid = inferred, true
		case exprlang.KindAny, exprlang.KindString, exprlang.KindNumber, exprlang.KindBool, exprlang.KindNull:
		}
		if first {
			result.ty = inferred
			first = false
		} else {
			result.ty = result.ty.Merge(inferred)
		}
	}
	return result
}
