// Package binding identifies host-prepared decisions without exposing their configuration.
package binding

type namespace struct{ marker byte }

// ID is an opaque decision identity. Its zero value is unbound.
type ID struct {
	owner *namespace
	index uint64
}

type Namespace struct{ owner *namespace }

func NewNamespace() Namespace { return Namespace{owner: &namespace{marker: 1}} }

func (n Namespace) ID(index uint64) ID { return ID{owner: n.owner, index: index} }

func (n Namespace) Index(id ID) (uint64, bool) {
	return id.index, n.owner != nil && id.owner == n.owner && id.index != 0
}

func (id ID) Valid() bool { return id.owner != nil && id.index != 0 }

func (id ID) Equal(other ID) bool { return id == other }
