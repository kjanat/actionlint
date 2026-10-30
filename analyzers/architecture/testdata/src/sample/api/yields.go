package api

import (
	"iter"
	"slices"
)

type Node struct {
	Key      string
	Children []*Node
}

type Field struct {
	Key  *string
	Node *Node
}

// Reader records diagnostics while it reads nodes passed to it.
type Reader struct{ errs []string }

// fields writes r.errs for children without a key, but yields only new keys
// and children of n, never storage of r.
func (r *Reader) fields(n *Node) iter.Seq[Field] {
	return func(yield func(Field) bool) {
		for _, c := range n.Children {
			if c.Key == "" {
				r.errs = append(r.errs, "empty key")
				continue
			}
			key := c.Key
			if !yield(Field{Key: &key, Node: c}) {
				return
			}
		}
	}
}

// fieldsIn returns the iterator of another method unchanged.
func (r *Reader) fieldsIn(n *Node) iter.Seq[Field] { return r.fields(n) }

// A loop body receives only what the iterator yields, not the receiver the
// iterator captured to write its diagnostics.
func (r *Reader) Keys(n *Node) []*string {
	var out []*string
	for f := range r.fieldsIn(n) {
		out = append(out, f.Key)
	}
	return out
}

// Collecting the iterator returns a new slice of what it yields.
func (r *Reader) Fields(n *Node) []Field { return slices.Collect(r.fields(n)) }

// errors yields the receiver's own slice.
func (r *Reader) errors() iter.Seq[*[]string] {
	return func(yield func(*[]string) bool) { yield(&r.errs) }
}

func (r *Reader) Errors() []*[]string { // want "ARC003: Errors result 1"
	var out []*[]string
	for e := range r.errors() {
		out = append(out, e)
	}
	return out
}

func (r *Reader) CollectedErrors() []*[]string { return slices.Collect(r.errors()) } // want "ARC003: CollectedErrors result 1"

// Collecting an iterator literal still applies what the literal stores into
// the variables it captured.
func (r *Reader) LastSeen() []string { // want "ARC003: LastSeen result 1"
	var last []string
	seq := func(yield func(int) bool) {
		last = r.errs
		yield(len(last))
	}
	_ = slices.Collect(seq)
	return last
}

// relayed hands its yield function to another iterator, so what it yields is
// not recorded and the loop body may receive anything the iterator captured.
func (r *Reader) relayed(n *Node) iter.Seq[Field] {
	return func(yield func(Field) bool) { r.fields(n)(yield) }
}

func (r *Reader) Relayed(n *Node) []Field { // want "ARC004: Relayed result 1"
	var out []Field
	for f := range r.relayed(n) {
		out = append(out, f)
	}
	return out
}
