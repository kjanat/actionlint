// Package related enforces related-location request grants and admission errors.
package related

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
)

// RejectedError counts withheld related locations, never primary diagnostics.
// The count is fixed at creation; read it with Count.
type RejectedError struct {
	withheld int
	cause    error
}

// NewRejectedError reports withheld related locations; nil when none were withheld.
func NewRejectedError(withheld int) error {
	if withheld <= 0 {
		return nil
	}
	return &RejectedError{withheld: withheld}
}

func (e *RejectedError) Error() string {
	message := fmt.Sprintf("admission-rejected: withheld %d related location(s)", e.withheld)
	if e.cause != nil {
		return message + ": " + e.cause.Error()
	}
	return message
}
func (e *RejectedError) Unwrap() error     { return e.cause }
func (*RejectedError) Code() string        { return "admission-rejected" }
func (e *RejectedError) Count() int        { return e.withheld }
func (e *RejectedError) relatedCount() int { return e.withheld }

// Consolidate exposes the complete count across request bridges and sealed
// admission while retaining every original operational cause for errors.Is/As.
func Consolidate(prior error, withheld int) error {
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
	return &RejectedError{withheld: total, cause: prior}
}

// Bind applies only the trusted exact-request-path grant. Explicit bindings are
// claims for sealed revalidation; mixed or unsupported bindings are rejected.
func Bind(locations []diagnostic.Related, path string, revision source.Revision, model source.Coordinates) ([]diagnostic.Related, error) {
	if locations == nil {
		return nil, nil
	}
	kept := make([]diagnostic.Related, 0, len(locations))
	count := 0
	for _, location := range locations {
		if location.Revision == (source.Revision{}) && location.Coordinates == "" {
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
		return kept, &RejectedError{withheld: count}
	}
	return kept, nil
}

// BindDiagnostics retains every primary and reports one exact withheld count.
func BindDiagnostics(findings []diagnostic.Diagnostic, path string, revision source.Revision, model source.Coordinates) error {
	count := 0
	for i := range findings {
		kept, err := Bind(findings[i].Related, path, revision, model)
		findings[i].Related = kept
		if rejected, ok := errors.AsType[*RejectedError](err); ok {
			count += rejected.Count()
		} else if err != nil {
			return err
		}
	}
	if count != 0 {
		return &RejectedError{withheld: count}
	}
	return nil
}
