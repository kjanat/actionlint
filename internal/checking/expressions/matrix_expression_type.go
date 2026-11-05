package expressions

import (
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

type expressionRefinement struct {
	ty                 exprtypes.Type
	state              value.State
	templateInvalid    exprtypes.Type
	hasTemplateInvalid bool
}

// expressionType keeps finite matrix-dependent expressions precise without
// choosing one row's value for another. Unknown or oversized matrices retain
// the ordinary semantic checker's type.
func (rule *Checker) expressionType(text string, evaluation exprvalues.EvaluationResult) expressionRefinement {
	switch evaluation.State {
	case exprvalues.EvaluationKnown:
		return expressionRefinement{ty: exprtypes.TypeOfJSONValue(evaluation.Value), state: value.Evaluated}
	case exprvalues.EvaluationInvalid, exprvalues.EvaluationNotAttempted:
		return expressionRefinement{state: value.Invalid}
	case exprvalues.EvaluationUnknown:
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
		inferred := matrixValueType(resolved, exprtypes.AnyType())
		switch inferred.Kind() {
		case exprtypes.KindObject, exprtypes.KindArray:
			result.templateInvalid, result.hasTemplateInvalid = inferred, true
		case exprtypes.KindAny, exprtypes.KindString, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindNull:
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
