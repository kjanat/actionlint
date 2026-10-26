package codec

import (
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

func (e *LoadError) Unwrap() error { return e.cause }

func (e *LoadError) Error() string {
	if e.text != "" {
		return e.text
	}
	return e.Message
}

type DecodeError struct {
	Errors []*LoadError
}

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
