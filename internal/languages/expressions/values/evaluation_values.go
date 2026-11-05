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
