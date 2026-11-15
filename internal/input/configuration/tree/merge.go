// Package tree overlays configuration values together with their source locations.
package tree

import (
	"maps"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

// Input identifies the source of a value and any inherited null reset.
type Input struct {
	Name, File         string
	Line, Column       int
	KeyLine, KeyColumn int
	Reset              *Input
}

// Mode describes how a field combines with the preceding layer.
type Mode uint8

const (
	Recursive Mode = iota
	Replace
	AtomicMembers
	KeyedList
)

// Rule assigns merge semantics to a JSON pointer. Members are reset when an
// atomic object follows null; Key identifies entries in a keyed list.
type Rule struct {
	Mode    Mode
	Members []string
	Key     string
}
type Plan map[string]Rule

// Node is a format-independent value paired with the origin of every child.
type Node struct {
	value    any
	input    Input
	hasInput bool
	object   map[string]*Node
	list     []*Node
}

func Capture(value any, inputs map[string]Input) *Node { return capture(value, inputs, "") }
func capture(value any, inputs map[string]Input, path string) *Node {
	in, ok := inputs[path]
	n := &Node{value: value, input: cloneInput(in), hasInput: ok}
	switch v := value.(type) {
	case map[string]any:
		n.object = make(map[string]*Node, len(v))
		for k, x := range v {
			n.object[k] = capture(x, inputs, document.Path(path, k))
		}
	case []any:
		n.list = make([]*Node, len(v))
		for i, x := range v {
			n.list[i] = capture(x, inputs, document.Index(path, i))
		}
	}
	return n
}

func (n *Node) Value() any {
	if n == nil {
		return nil
	}
	if n.object != nil {
		o := make(map[string]any, len(n.object))
		for k, v := range n.object {
			o[k] = v.Value()
		}
		return o
	}
	if n.list != nil {
		o := make([]any, len(n.list))
		for i, v := range n.list {
			o[i] = v.Value()
		}
		return o
	}
	return n.value
}

func (n *Node) Origins() map[string]Input {
	out := map[string]Input{}
	var visit func(*Node, string)
	visit = func(n *Node, path string) {
		if n == nil {
			return
		}
		if n.hasInput {
			out[path] = cloneInput(n.input)
		}
		for k, v := range n.object {
			visit(v, document.Path(path, k))
		}
		for i, v := range n.list {
			visit(v, document.Index(path, i))
		}
	}
	visit(n, "")
	return out
}

func (n *Node) Child(key string) *Node {
	if n == nil {
		return nil
	}
	return n.object[key]
}

func cloneInput(input Input) Input {
	if input.Reset != nil {
		reset := cloneInput(*input.Reset)
		input.Reset = &reset
	}
	return input
}

func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	out := *n
	out.input = cloneInput(n.input)
	if n.object != nil {
		out.object = make(map[string]*Node, len(n.object))
		for k, v := range n.object {
			out.object[k] = cloneNode(v)
		}
	}
	if n.list != nil {
		out.list = make([]*Node, len(n.list))
		for i, v := range n.list {
			out.list[i] = cloneNode(v)
		}
	}
	return &out
}

// Overlay returns a detached combined tree according to plan.
func Overlay(base, patch *Node, plan Plan) *Node { return overlay(base, patch, plan, "") }

func overlay(base, patch *Node, plan Plan, path string) *Node {
	if patch == nil {
		return cloneNode(base)
	}
	if base == nil {
		return cloneNode(patch)
	}
	rule := plan[path]
	if rule.Mode == Replace {
		return cloneNode(patch)
	}
	if rule.Mode == KeyedList && patch.list != nil && len(patch.list) > 0 {
		out := cloneNode(patch)
		out.list = make([]*Node, 0, len(base.list)+len(patch.list))
		index := map[string]int{}
		key := func(n *Node) string {
			v := n.Child(rule.Key)
			if v == nil {
				return ""
			}
			s, _ := v.value.(string)
			return s
		}
		for _, v := range base.list {
			index[key(v)] = len(out.list)
			out.list = append(out.list, cloneNode(v))
		}
		for _, v := range patch.list {
			id := key(v)
			if i, ok := index[id]; ok {
				out.list[i] = cloneNode(v)
			} else {
				index[id] = len(out.list)
				out.list = append(out.list, cloneNode(v))
			}
		}
		return out
	}
	if patch.object == nil {
		return cloneNode(patch)
	}
	out := cloneNode(patch)
	out.object = map[string]*Node{}
	if base.object != nil {
		for k, v := range base.object {
			out.object[k] = cloneNode(v)
		}
	}
	if base.value == nil && base.object == nil && base.list == nil {
		reset := base.input
		if out.hasInput && base.hasInput {
			out.input.Reset = &reset
		}
		if rule.Mode == AtomicMembers {
			for _, k := range rule.Members {
				out.object[k] = &Node{input: reset, hasInput: base.hasInput}
			}
		}
	} else if base.hasInput && base.input.Reset != nil && out.hasInput {
		out.input.Reset = base.input.Reset
	}
	for k, v := range patch.object {
		if rule.Mode == AtomicMembers {
			out.object[k] = cloneNode(v)
		} else {
			out.object[k] = overlay(base.Child(k), v, plan, document.Path(path, k))
		}
	}
	return out
}

// Prefix returns origins under a JSON pointer with that prefix removed.
func Prefix(origins map[string]Input, prefix string) map[string]Input {
	out := maps.Clone(origins)
	for k := range out {
		delete(out, k)
	}
	for k, v := range origins {
		if k == prefix || strings.HasPrefix(k, prefix+"/") {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out
}
