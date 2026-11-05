package report

import (
	"fmt"
	"slices"
	"strings"
)

// OverlayError reports individually valid inputs whose combined configuration
// is invalid. It preserves the validation cause for errors.Is and errors.As.
type OverlayError struct {
	inputs []string
	cause  error
}

// NewOverlayError copies the input names and stores the validation cause.
// A nil cause means that no overlay failure occurred.
func NewOverlayError(inputs []string, cause error) *OverlayError {
	if cause == nil {
		return nil
	}
	return &OverlayError{inputs: slices.Clone(inputs), cause: cause}
}

func (e *OverlayError) Error() string {
	return fmt.Sprintf("configuration after inputs %s: %v", strings.Join(e.inputs, ", "), e.cause)
}

// Unwrap returns the configuration validation error.
func (e *OverlayError) Unwrap() error { return e.cause }
