package yaml

import (
	"errors"
	"io"

	upstream "go.yaml.in/yaml/v4"
)

func fromParser(root *upstream.Node) *Node {
	seen := map[*upstream.Node]*Node{}
	var convert func(*upstream.Node) *Node
	convert = func(n *upstream.Node) *Node {
		if n == nil {
			return nil
		}
		if old := seen[n]; old != nil {
			return old
		}
		r := &Node{Kind: Kind(n.Kind), Style: Style(n.Style), Tag: n.Tag, Value: n.Value, Anchor: n.Anchor, HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment, Line: n.Line, Column: n.Column}
		seen[n] = r
		r.Alias = convert(n.Alias)
		for _, c := range n.Content {
			r.Content = append(r.Content, convert(c))
		}
		return r
	}
	return convert(root)
}
func toParser(root *Node) *upstream.Node {
	seen := map[*Node]*upstream.Node{}
	var convert func(*Node) *upstream.Node
	convert = func(n *Node) *upstream.Node {
		if n == nil {
			return nil
		}
		if old := seen[n]; old != nil {
			return old
		}
		r := &upstream.Node{Kind: upstream.Kind(n.Kind), Style: upstream.Style(n.Style), Tag: n.Tag, Value: n.Value, Anchor: n.Anchor, HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment, Line: n.Line, Column: n.Column}
		seen[n] = r
		r.Alias = convert(n.Alias)
		for _, c := range n.Content {
			r.Content = append(r.Content, convert(c))
		}
		return r
	}
	return convert(root)
}
func translate(err error) error {
	if err == nil {
		return nil
	}
	if list, ok := errors.AsType[*upstream.LoadErrors](err); ok {
		r := &DecodeError{text: err.Error()}
		for _, e := range list.Errors {
			r.Errors = append(r.Errors, &LoadError{Message: e.Message, Mark: Mark{e.Mark.Index, e.Mark.Line, e.Mark.Column}, text: e.Error()})
		}
		return r
	}
	if e, ok := errors.AsType[*upstream.LoadError](err); ok {
		return &LoadError{Message: e.Message, Mark: Mark{e.Mark.Index, e.Mark.Line, e.Mark.Column}, text: e.Error()}
	}
	return err
}
func Unmarshal(data []byte, target any) error {
	var n upstream.Node
	if err := upstream.Unmarshal(data, &n); err != nil {
		return translate(err)
	}
	if n.Kind == 0 {
		return nil
	}
	owned := fromParser(&n)
	if p, ok := target.(*Node); ok {
		*p = *owned
		return nil
	}
	return owned.Decode(target)
}
func (n *Node) Encode(value any) error {
	var raw upstream.Node
	if err := raw.Encode(value); err != nil {
		return translate(err)
	}
	*n = *fromParser(&raw)
	return nil
}
func Marshal(value any) ([]byte, error) {
	if n, ok := value.(*Node); ok {
		return upstream.Marshal(toParser(n))
	}
	return upstream.Marshal(value)
}

type Encoder struct{ inner *upstream.Encoder }

func NewEncoder(out io.Writer) *Encoder { return &Encoder{upstream.NewEncoder(out)} }
func (e *Encoder) Encode(value any) error {
	if n, ok := value.(*Node); ok {
		return e.inner.Encode(toParser(n))
	}
	return e.inner.Encode(value)
}
func (e *Encoder) SetIndent(n int) { e.inner.SetIndent(n) }
func (e *Encoder) Close() error    { return e.inner.Close() }
