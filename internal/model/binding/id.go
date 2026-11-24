// Package binding identifies host-prepared decisions without exposing their configuration.
package binding

type namespace struct{ marker byte }

// ID is an opaque decision identity. Its zero value is unbound.
type ID struct {
	origin *namespace
	index  uint64
}

type Namespace struct{ origin *namespace }

func NewNamespace() Namespace { return Namespace{origin: &namespace{marker: 1}} }

func (n Namespace) ID(index uint64) ID { return ID{origin: n.origin, index: index} }

func (n Namespace) Index(id ID) (uint64, bool) {
	return id.index, n.origin != nil && id.origin == n.origin && id.index != 0
}

func (id ID) Valid() bool { return id.origin != nil && id.index != 0 }

func (id ID) Equal(other ID) bool { return id == other }
