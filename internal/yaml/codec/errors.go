package codec

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Mark struct{ Index, Line, Column int }

type EncodeError struct {
	Message string
	cause   error
}

func (e *EncodeError) Error() string { return e.Message }
func (e *EncodeError) Unwrap() error { return e.cause }

type LoadError struct {
	Message string
	Mark    Mark
	text    string
	cause   error
}

// FatalLoadError distinguishes aborting loads from collected schema diagnostics.
func FatalLoadError(err error) (*LoadError, bool) {
	if admission, rejected := errors.AsType[*AdmissionError](err); rejected {
		if loaded, ok := errors.AsType[*LoadError](admission); ok {
			return loaded, true
		}
		return &LoadError{Message: admission.Error(), cause: admission}, true
	}
	if _, collected := errors.AsType[*DecodeError](err); collected {
		return nil, false
	}
	return errors.AsType[*LoadError](err)
}

func (e *LoadError) Unwrap() error { return e.cause }

func (e *LoadError) diagnostics() []*LoadError { return []*LoadError{e} }

func (e *LoadError) Error() string {
	if e.text != "" {
		return e.text
	}
	return e.Message
}

type DecodeError struct {
	Errors []*LoadError
}

func (e *DecodeError) diagnostics() []*LoadError { return e.Errors }

func (e *DecodeError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, item := range e.Errors {
		if item.Mark.Line > 0 {
			parts[i] = fmt.Sprintf("line %d: %s", item.Mark.Line, item.Message)
		} else {
			parts[i] = "<unknown position>: " + item.Message
		}
	}
	return "yaml: construct errors: " + strings.Join(parts, "; ")
}

func (e *DecodeError) Unwrap() []error {
	result := make([]error, len(e.Errors))
	for i, item := range e.Errors {
		result[i] = item
	}
	return result
}

// LoadDiagnostics preserves wrapper context, source marks and joined-cause order.
func LoadDiagnostics(err error) []*LoadError {
	return locateDecodeErrors(err, Mark{}).Errors
}

// FormatLoadError preserves native load context and renders collected diagnostics.
func FormatLoadError(err error) string {
	if err == nil {
		return ""
	}
	_, joined := err.(interface{ Unwrap() []error })
	if _, collected := errors.AsType[*DecodeError](err); !joined && !collected {
		return err.Error()
	}
	diagnostics := LoadDiagnostics(err)
	if len(diagnostics) == 1 {
		item := diagnostics[0]
		if item.Mark.Line > 0 {
			return fmt.Sprintf("line %d: %s", item.Mark.Line, item.Message)
		}
		return item.Message
	}
	return strings.ReplaceAll((&DecodeError{Errors: diagnostics}).Error(), "\n", "")
}

func locateDecodeErrors(err error, fallback Mark) *DecodeError {
	result := &DecodeError{}
	if err == nil {
		return result
	}
	if value, ok := err.(interface{ diagnostics() []*LoadError }); ok {
		for _, original := range value.diagnostics() {
			item := *original
			if item.Mark.Line == 0 && item.Mark.Column == 0 {
				item.Mark = fallback
				item.text = ""
			}
			item.cause = original
			result.Errors = append(result.Errors, &item)
		}
	} else if value, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range value.Unwrap() {
			result.Errors = append(result.Errors, locateDecodeErrors(child, fallback).Errors...)
		}
	} else {
		if child := errors.Unwrap(err); child != nil {
			result = locateDecodeErrors(child, fallback)
			for _, item := range result.Errors {
				if strings.Contains(err.Error(), child.Error()) {
					item.Message = strings.Replace(err.Error(), child.Error(), item.Message, 1)
				} else {
					item.Message = err.Error() + ": " + item.Message
				}
				item.text, item.cause = "", err
			}
		} else {
			result.Errors = append(result.Errors, &LoadError{Message: err.Error(), Mark: fallback, cause: err})
		}
	}
	return result
}

func joinDecodeErrors(errs ...error) error {
	var result DecodeError
	var collect func(error)
	collect = func(err error) {
		if err == nil {
			return
		}
		if local, ok := err.(interface{ diagnostics() []*LoadError }); ok {
			result.Errors = append(result.Errors, local.diagnostics()...)
		} else if list, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range list.Unwrap() {
				collect(child)
			}
		} else {
			if inner := errors.Unwrap(err); inner != nil {
				_, decode := errors.AsType[*DecodeError](inner)
				_, load := errors.AsType[*LoadError](inner)
				if decode || load {
					collect(inner)
					return
				}
			}
			result.Errors = append(result.Errors, &LoadError{Message: err.Error(), cause: err})
		}
	}
	for _, err := range errs {
		collect(err)
	}
	if len(result.Errors) == 0 {
		return nil
	}
	slices.SortStableFunc(result.Errors, func(a, b *LoadError) int {
		if order := cmp.Compare(a.Mark.Line, b.Mark.Line); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Mark.Column, b.Mark.Column); order != 0 {
			return order
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return &result
}

type GraphError struct {
	Mark   Mark
	Reason string
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("YAML graph at %d:%d: %s", e.Mark.Line, e.Mark.Column, e.Reason)
}
