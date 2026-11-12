package values

// EvaluationState distinguishes values, unknown results, runtime failures and
// expressions whose static validation prevented evaluation.
type EvaluationState uint8

const (
	EvaluationUnknown EvaluationState = iota
	EvaluationKnown
	EvaluationInvalid
	// EvaluationNotAttempted means static validation prevented evaluation.
	EvaluationNotAttempted
)

// EvaluationResult records a value only when its state is EvaluationKnown.
type EvaluationResult struct {
	State EvaluationState
	Value any
}

// Clone copies arrays and objects so callers can edit the returned runtime value.
func (result EvaluationResult) Clone() EvaluationResult {
	result.Value = cloneRuntimeValue(result.Value)
	return result
}

func cloneRuntimeValue(value any) any {
	switch value := value.(type) {
	case []any:
		if value == nil {
			return []any(nil)
		}
		copied := make([]any, len(value))
		for index, item := range value {
			copied[index] = cloneRuntimeValue(item)
		}
		return copied
	case map[string]any:
		if value == nil {
			return map[string]any(nil)
		}
		copied := make(map[string]any, len(value))
		for key, item := range value {
			copied[key] = cloneRuntimeValue(item)
		}
		return copied
	default:
		return value
	}
}
