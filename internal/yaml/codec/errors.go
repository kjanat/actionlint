package codec

import (
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

type Unmarshaler interface {
	UnmarshalYAML(*Decoder, *syntax.Node) error
}

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
