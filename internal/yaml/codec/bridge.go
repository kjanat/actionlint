// Package codec converts YAML sources and Go values through an owned syntax graph.
package codec

import (
	"errors"
	"io"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "github.com/kjanat/go-yaml/v4"
)

func fromParser(root *upstream.Node, seen map[*upstream.Node]*syntax.Node) (*syntax.Node, error) {
	if seen == nil {
		seen = map[*upstream.Node]*syntax.Node{}
	}
	var convert func(*upstream.Node) (*syntax.Node, error)
	convert = func(n *upstream.Node) (*syntax.Node, error) {
		if n == nil {
			return nil, nil
		}
		if old := seen[n]; old != nil {
			return old, nil
		}
		kind, err := ownedKind(n.Kind)
		if err != nil {
			return nil, err
		}
		style, err := ownedStyle(n.Style)
		if err != nil {
			return nil, err
		}
		r := &syntax.Node{Kind: kind, Style: style, Tag: n.Tag, Value: n.Value, Anchor: n.Anchor, HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment, Line: n.Line, Column: n.Column}
		seen[n] = r
		r.Alias, err = convert(n.Alias)
		if err != nil {
			return nil, err
		}
		for _, c := range n.Content {
			child, err := convert(c)
			if err != nil {
				return nil, err
			}
			r.Content = append(r.Content, child)
		}
		return r, nil
	}
	return convert(root)
}

type parserGraph struct {
	root      *upstream.Node
	originals map[*upstream.Node]*syntax.Node
}

func toParser(root *syntax.Node) (parserGraph, error) {
	if err := validateGraph(root); err != nil {
		return parserGraph{}, err
	}
	graph := parserGraph{originals: map[*upstream.Node]*syntax.Node{}}
	seen := map[*syntax.Node]*upstream.Node{}
	var convert func(*syntax.Node) (*upstream.Node, error)
	convert = func(n *syntax.Node) (*upstream.Node, error) {
		if n == nil {
			return nil, nil
		}
		if old := seen[n]; old != nil {
			return old, nil
		}
		kind, err := parserKind(n.Kind)
		if err != nil {
			return nil, err
		}
		style, err := parserStyle(n.Style)
		if err != nil {
			return nil, err
		}
		r := &upstream.Node{Kind: kind, Style: style, Tag: n.Tag, Value: n.Value, Anchor: n.Anchor, HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment, Line: n.Line, Column: n.Column}
		seen[n] = r
		graph.originals[r] = n
		r.Alias, err = convert(n.Alias)
		if err != nil {
			return nil, err
		}
		for _, c := range n.Content {
			child, err := convert(c)
			if err != nil {
				return nil, err
			}
			r.Content = append(r.Content, child)
		}
		return r, nil
	}
	var err error
	graph.root, err = convert(root)
	return graph, err
}

func translate(err error) error {
	if err == nil {
		return nil
	}
	if list, ok := errors.AsType[*upstream.LoadErrors](err); ok {
		r := &DecodeError{}
		for _, e := range list.Errors {
			r.Errors = append(r.Errors, &LoadError{Message: e.Message, Mark: Mark{e.Mark.Index, e.Mark.Line, e.Mark.Column}, text: e.Error(), cause: e})
		}
		return r
	}
	if e, ok := errors.AsType[*upstream.LoadError](err); ok {
		return &LoadError{Message: e.Message, Mark: Mark{e.Mark.Index, e.Mark.Line, e.Mark.Column}, text: e.Error(), cause: e}
	}
	return err
}

// Parse reads the first YAML document into an owned syntax graph.
// Empty input returns a non-nil zero-kind node; malformed YAML returns nil and its parse error.
func Parse(data []byte) (*syntax.Node, error) {
	var node upstream.Node
	if err := upstream.Unmarshal(data, &node); err != nil {
		return nil, translate(err)
	}
	if node.Kind == 0 {
		return &syntax.Node{}, nil
	}
	return fromParser(&node, nil)
}

func encodeError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*EncodeError](err); ok {
		return err
	}
	return &EncodeError{Message: err.Error(), cause: err}
}

func representation(value any) (*upstream.Node, error) {
	value, err := encodingValue(value)
	if err != nil {
		return nil, encodeError(err)
	}
	var raw upstream.Node
	if err := raw.Encode(value); err != nil {
		return nil, encodeError(err)
	}
	return &raw, nil
}

func Encode(value any) (*syntax.Node, error) {
	raw, err := representation(value)
	if err != nil {
		return nil, err
	}
	node, err := fromParser(raw, nil)
	return node, encodeError(err)
}

func Marshal(value any) ([]byte, error) {
	value, err := representation(value)
	if err != nil {
		return nil, err
	}
	data, err := upstream.Marshal(value)
	return data, encodeError(err)
}

type Encoder struct{ inner *upstream.Encoder }

func NewEncoder(out io.Writer) *Encoder { return &Encoder{upstream.NewEncoder(out)} }
func (e *Encoder) Encode(value any) error {
	value, err := representation(value)
	if err != nil {
		return err
	}
	return encodeError(e.inner.Encode(value))
}
func (e *Encoder) SetIndent(n int) { e.inner.SetIndent(n) }
func (e *Encoder) Close() error    { return encodeError(e.inner.Close()) }
