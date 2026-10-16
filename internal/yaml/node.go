// Package yaml owns the syntax tree used by semantic analysis. Parser-specific
// nodes and decoding hooks stay in the bridge; analysis can copy and normalize
// this graph without sharing mutable state with a parser implementation.
package yaml

import (
	"fmt"
	"strings"
)

type Kind uint32

const (
	DocumentNode Kind = 1 << iota
	SequenceNode
	MappingNode
	ScalarNode
	AliasNode
)

type Style uint32

const (
	TaggedStyle Style = 1 << iota
	DoubleQuotedStyle
	SingleQuotedStyle
	LiteralStyle
	FoldedStyle
	FlowStyle
)

type Node struct {
	Kind                                  Kind
	Style                                 Style
	Tag, Value, Anchor                    string
	Alias                                 *Node
	Content                               []*Node
	HeadComment, LineComment, FootComment string
	Line, Column                          int
}
type Unmarshaler interface{ UnmarshalYAML(*Node) error }
type Mark struct{ Index, Line, Column int }
type LoadError struct {
	Message string
	Mark    Mark
	text    string
}

func (e *LoadError) Error() string {
	if e.text != "" {
		return e.text
	}
	return e.Message
}

type DecodeError struct {
	Errors []*LoadError
	text   string
}

func (e *DecodeError) Error() string {
	if e.text != "" {
		return e.text
	}
	parts := make([]string, len(e.Errors))
	for i, item := range e.Errors {
		parts[i] = fmt.Sprintf("line %d: %s", item.Mark.Line, item.Message)
	}
	return "yaml: construct errors: " + strings.Join(parts, "; ")
}
