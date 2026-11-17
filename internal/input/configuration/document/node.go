// Package document reads configuration files into one tree of values and
// source locations. Decoding, inheritance merging, overlays, migrations and
// format-preserving edits all use this tree.
package document

import (
	"maps"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// Location records where a configuration value came from. File names the
// source file; Name names a command-line input. Value points
// at the value and Key at its key or list item; a zero position is unknown.
// Reset points at an inherited null that this value replaced.
type Location struct {
	Name, File string
	Value, Key sourcetext.Position
	Reset      *Location
}

// Node is one configuration value together with the location of every child.
// Parse, Load and Capture build nodes; Value and Origins read them back.
type Node struct {
	value    any
	location Location
	located  bool
	object   map[string]*Node
	list     []*Node
}

// Path appends an object key to a JSON pointer, escaping "~" and "/".
func Path(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// Index appends a list index to a JSON pointer.
func Index(parent string, i int) string { return Path(parent, strconv.Itoa(i)) }

// Capture builds a node from a value and the locations of its JSON pointers.
// Pointers without a location produce unlocated nodes.
func Capture(value any, locations map[string]Location) *Node {
	return capture(value, locations, "")
}

func capture(value any, locations map[string]Location, path string) *Node {
	at, ok := locations[path]
	n := &Node{value: value, location: cloneLocation(at), located: ok}
	switch v := value.(type) {
	case map[string]any:
		n.object = make(map[string]*Node, len(v))
		for k, x := range v {
			n.object[k] = capture(x, locations, Path(path, k))
		}
	case []any:
		n.list = make([]*Node, len(v))
		for i, x := range v {
			n.list[i] = capture(x, locations, Index(path, i))
		}
	}
	return n
}

// Value returns the node as plain Go values: map[string]any, []any, string,
// bool, json.Number, float64 or nil.
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

// Origins returns the location of every located node by JSON pointer.
func (n *Node) Origins() map[string]Location {
	out := map[string]Location{}
	var visit func(*Node, string)
	visit = func(n *Node, path string) {
		if n == nil {
			return
		}
		if n.located {
			out[path] = cloneLocation(n.location)
		}
		for k, v := range n.object {
			visit(v, Path(path, k))
		}
		for i, v := range n.list {
			visit(v, Index(path, i))
		}
	}
	visit(n, "")
	return out
}

// Child returns the member named key, or nil when n is not an object or has no such member.
func (n *Node) Child(key string) *Node {
	if n == nil {
		return nil
	}
	return n.object[key]
}

// setFile records file as the source of every located node.
func (n *Node) setFile(file string) {
	if n == nil {
		return
	}
	if n.located {
		n.location.File = file
	}
	for _, child := range n.object {
		child.setFile(file)
	}
	for _, child := range n.list {
		child.setFile(file)
	}
}

func cloneLocation(location Location) Location {
	if location.Reset != nil {
		reset := cloneLocation(*location.Reset)
		location.Reset = &reset
	}
	return location
}

func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	out := *n
	out.location = cloneLocation(n.location)
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

// Merge returns a detached tree that applies patch over base according to plan.
func Merge(base, patch *Node, plan Plan) *Node { return merge(base, patch, plan, "") }

func merge(base, patch *Node, plan Plan, path string) *Node {
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
		reset := base.location
		if out.located && base.located {
			out.location.Reset = &reset
		}
		if rule.Mode == AtomicMembers {
			for _, k := range rule.Members {
				out.object[k] = &Node{location: reset, located: base.located}
			}
		}
	} else if base.located && base.location.Reset != nil && out.located {
		out.location.Reset = base.location.Reset
	}
	for k, v := range patch.object {
		if rule.Mode == AtomicMembers {
			out.object[k] = cloneNode(v)
		} else {
			out.object[k] = merge(base.Child(k), v, plan, Path(path, k))
		}
	}
	return out
}

// Prefix returns the locations under a JSON pointer with that prefix removed.
func Prefix(origins map[string]Location, prefix string) map[string]Location {
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
