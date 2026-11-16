package values

import (
	"math"
	"slices"
	"unicode/utf16"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/json"
)

func ExpressionTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0 && !math.IsNaN(v)
	case string:
		return v != ""
	default:
		return true
	}
}

func CompareConditionValues(kind syntax.CompareOpNodeKind, left, right any) (bool, bool) {
	if l, ok := left.(string); ok {
		if r, ok := right.(string); ok {
			order := slices.Compare(utf16.Encode([]rune(json.OrdinalIgnoreCaseKey(l))), utf16.Encode([]rune(json.OrdinalIgnoreCaseKey(r))))
			return compareConditionNumbers(kind, float64(order), 0), true
		}
	}
	composite := func(value any) bool {
		switch value.(type) {
		case map[string]any, []any:
			return true
		default:
			return false
		}
	}
	// Two composite values can require identity tracking. Against a primitive,
	// their numeric coercion is NaN and the comparison has a known result.
	if composite(left) && composite(right) {
		return false, false
	}
	return compareConditionNumbers(kind, ToNumber(left), ToNumber(right)), true
}

func compareConditionNumbers(kind syntax.CompareOpNodeKind, a, b float64) bool {
	switch kind {
	case syntax.CompareOpNodeKindEq:
		return a == b
	case syntax.CompareOpNodeKindNotEq:
		return a != b
	case syntax.CompareOpNodeKindLess:
		return a < b
	case syntax.CompareOpNodeKindLessEq:
		return a <= b
	case syntax.CompareOpNodeKindGreater:
		return a > b
	case syntax.CompareOpNodeKindGreaterEq:
		return a >= b
	case syntax.CompareOpNodeKindInvalid:
		// An invalid operator never holds.
		fallthrough
	default:
		return false
	}
}
