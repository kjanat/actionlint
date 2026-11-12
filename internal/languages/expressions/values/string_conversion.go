package values

import (
	"math"
	"strconv"
)

// PredicateString converts a scalar when its spelling is known across runner
// JSON modes. The boolean is false when numeric formatting is uncertain.
func PredicateString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	case bool:
		return strconv.FormatBool(value), true
	case float64:
		// These integers have the same decimal representation under the
		// runner's invariant G15 format and round-trip number formatting.
		if math.Abs(value) < 1e15 && math.Trunc(value) == value && (value != 0 || !math.Signbit(value)) {
			return strconv.FormatFloat(value, 'f', 0, 64), true
		}
	}
	return "", false
}

// FunctionString converts a known scalar or composite to the string consumed by
// a function parameter. The boolean reports whether the spelling is known.
func FunctionString(value any) (string, bool) {
	if s, ok := PredicateString(value); ok {
		return s, true
	}
	switch value.(type) {
	case []any, map[string]any:
		return JoinString(value)
	default:
		return "", false
	}
}

// JoinString converts values using join's composite spellings. Numeric spellings
// are unknown because runner serialization modes can differ.
func JoinString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	case bool:
		return strconv.FormatBool(value), true
	case []any:
		return "Array", true
	case map[string]any:
		return "Object", true
	default:
		// Do not assume an engine's numeric serialization, including signed zero.
		return "", false
	}
}
