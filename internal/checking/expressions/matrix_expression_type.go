package expressions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
)

type expressionRefinement struct {
	ty                 types.Type
	state              value.State
	templateInvalid    types.Type
	hasTemplateInvalid bool
}

// expressionType keeps finite matrix-dependent expressions precise without
// choosing one row's value for another. Unknown or oversized matrices retain
// the ordinary semantic checker's type.
func (rule *Checker) expressionType(text string, evaluation values.EvaluationResult) expressionRefinement {
	switch evaluation.State {
	case values.EvaluationKnown:
		return expressionRefinement{ty: types.TypeOfJSONValue(evaluation.Value), state: value.Evaluated}
	case values.EvaluationInvalid, values.EvaluationNotAttempted:
		return expressionRefinement{state: value.Invalid}
	case values.EvaluationUnknown:
	}
	if !rule.scriptJob.Present() {
		return expressionRefinement{state: value.Dynamic}
	}
	combinations, complete := rule.matrixResolution(rule.scriptJob.Strategy).Combinations(256)
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
		inferred := resolved.TypeWithFallback(types.AnyType())
		switch inferred.Kind() {
		case types.KindObject, types.KindArray:
			result.templateInvalid, result.hasTemplateInvalid = inferred, true
		case types.KindAny, types.KindString, types.KindNumber, types.KindBool, types.KindNull:
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
