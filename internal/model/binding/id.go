// Package binding identifies host-prepared decisions without exposing their configuration.
package binding

type namespace struct{ marker byte }

// ID is an opaque decision identity. Its zero value is unbound.
type ID struct {
	owner *namespace
	index uint64
}

// Namespace creates identities belonging to a single analysis registry.
type Namespace struct{ owner *namespace }

// NewNamespace allocates a distinct registry identity.
func NewNamespace() Namespace { return Namespace{owner: &namespace{marker: 1}} }

// ID names a registry entry. The owner validates the index when looking it up.
func (n Namespace) ID(index uint64) ID { return ID{owner: n.owner, index: index} }

// Index accepts only identities issued by this registry.
func (n Namespace) Index(id ID) (uint64, bool) {
	return id.index, n.owner != nil && id.owner == n.owner && id.index != 0
}

// Valid distinguishes a prepared identity from an unbound observation.
func (id ID) Valid() bool { return id.owner != nil && id.index != 0 }

// Equal compares registry and decision identity.
func (id ID) Equal(other ID) bool { return id == other }
