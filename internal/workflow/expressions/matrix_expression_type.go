package expressions

import exprlang "actionlint.kjanat.dev/v2/internal/expression"

// expressionType keeps finite matrix-dependent expressions precise without
// choosing one row's value for another. Unknown or oversized matrices retain
// the ordinary semantic checker's type.
func (rule *Checker) expressionType(text string) (exprlang.Type, exprlang.Type, bool) {
	if literal, known := expressionLiteral(text); known {
		return exprlang.TypeOfJSONValue(literal), exprlang.AnyType(), true
	}
	if !rule.scriptJob.Present() {
		return exprlang.AnyType(), exprlang.AnyType(), false
	}
	combinations, complete := rule.matrixResolution(rule.scriptJob.Strategy()).Combinations(256)
	if !complete || len(combinations) == 0 {
		return exprlang.AnyType(), exprlang.AnyType(), false
	}
	result := exprlang.AnyType()
	invalid := exprlang.AnyType()
	allKnown, first := true, true
	for _, combination := range combinations {
		resolved := combination.Evaluate(text)
		if !resolved.Known() {
			allKnown = false
			continue
		}
		inferred := matrixValueType(resolved, exprlang.AnyType())
		switch inferred.Kind() {
		case exprlang.KindObject, exprlang.KindArray:
			invalid = inferred
		case exprlang.KindAny, exprlang.KindString, exprlang.KindNumber, exprlang.KindBool, exprlang.KindNull:
		}
		if first {
			result = inferred
			first = false
		} else {
			result = result.Merge(inferred)
		}
	}
	if !allKnown {
		return exprlang.AnyType(), invalid, false
	}
	return result, invalid, true
}
