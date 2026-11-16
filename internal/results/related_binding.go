package results

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
)

// RejectedRelatedError counts withheld related locations, never primary diagnostics.
// The count is fixed at creation; read it with Count.
type RejectedRelatedError struct {
	withheld int
	cause    error
}

// NewRejectedRelatedError reports withheld related locations; nil when none were withheld.
func NewRejectedRelatedError(withheld int) error {
	if withheld <= 0 {
		return nil
	}
	return &RejectedRelatedError{withheld: withheld}
}

func (e *RejectedRelatedError) Error() string {
	message := fmt.Sprintf("admission-rejected: withheld %d related location(s)", e.withheld)
	if e.cause != nil {
		return message + ": " + e.cause.Error()
	}
	return message
}
func (e *RejectedRelatedError) Unwrap() error     { return e.cause }
func (*RejectedRelatedError) Code() string        { return "admission-rejected" }
func (e *RejectedRelatedError) Count() int        { return e.withheld }
func (e *RejectedRelatedError) relatedCount() int { return e.withheld }

// ConsolidateRejectedRelated exposes the complete count across request bridges and sealed
// admission while retaining every original operational cause for errors.Is/As.
func ConsolidateRejectedRelated(prior error, withheld int) error {
	var count func(error) int
	count = func(err error) int {
		if err == nil {
			return 0
		}
		// Aggregate counts already include their causes; do not descend twice.
		if aggregate, ok := err.(interface{ relatedCount() int }); ok {
			return aggregate.relatedCount()
		}
		if group, ok := err.(interface{ Unwrap() []error }); ok {
			n := 0
			for _, cause := range group.Unwrap() {
				n += count(cause)
			}
			return n
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return count(wrapped.Unwrap())
		}
		return 0
	}
	total := withheld + count(prior)
	if total == 0 {
		return prior
	}
	return &RejectedRelatedError{withheld: total, cause: prior}
}

// BindRelated applies only the trusted exact-request-path grant. Explicit bindings are
// claims for sealed revalidation; mixed or unsupported bindings are rejected.
func BindRelated(locations []Related, path string, revision sourcetext.Revision, model sourcetext.Coordinates) ([]Related, error) {
	if locations == nil {
		return nil, nil
	}
	kept := make([]Related, 0, len(locations))
	count := 0
	for _, location := range locations {
		if location.Revision == (sourcetext.Revision{}) && location.Coordinates == "" {
			if path == "" || location.Path != path || !revision.Valid() || !model.Valid() {
				count++
				continue
			}
			location.Revision, location.Coordinates = revision, model
		} else if !location.Revision.Valid() || !location.Coordinates.Valid() {
			count++
			continue
		}
		kept = append(kept, location)
	}
	if count != 0 {
		return kept, &RejectedRelatedError{withheld: count}
	}
	return kept, nil
}

// BindRelatedInDiagnostics retains every primary and reports one exact withheld count.
func BindRelatedInDiagnostics(findings []Diagnostic, path string, revision sourcetext.Revision, model sourcetext.Coordinates) error {
	count := 0
	for i := range findings {
		kept, err := BindRelated(findings[i].Related, path, revision, model)
		findings[i].Related = kept
		if rejected, ok := errors.AsType[*RejectedRelatedError](err); ok {
			count += rejected.Count()
		} else if err != nil {
			return err
		}
	}
	if count != 0 {
		return &RejectedRelatedError{withheld: count}
	}
	return nil
}
