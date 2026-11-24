package results

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type RejectedRelatedError struct {
	withheld int
	cause    error
}

// NewRejectedRelatedError returns nil when none were withheld.
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

// ConsolidateRejectedRelated keeps every original cause visible to errors.Is and errors.As.
func ConsolidateRejectedRelated(prior error, withheld int) error {
	var count func(error) int
	count = func(err error) int {
		if err == nil {
			return 0
		}
		// Aggregate counts already include their causes.
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
