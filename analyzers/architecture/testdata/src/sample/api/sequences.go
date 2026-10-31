package api

import (
	"iter"
	"maps"
	"slices"
)

type Member struct{ Score int }

// Club reads its members through maps.Keys and maps.Values. Those iterators
// only read the map, and slices.Sorted and slices.Collect build new slices.
type Club struct{ members map[string]*Member }

// Names copies string keys, so no receiver storage is exposed.
func (c *Club) Names() []string { return slices.Sorted(maps.Keys(c.members)) }

// Keys yields only strings, so the iterator exposes no receiver storage.
func (c *Club) Keys() iter.Seq[string] { return maps.Keys(c.members) }

// Members is a new slice, but its elements point at the receiver's members.
func (c *Club) Members() []*Member { return slices.Collect(maps.Values(c.members)) } // want "ARC003: Members result 1"

// Values yields the receiver's member pointers.
func (c *Club) Values() iter.Seq[*Member] { return maps.Values(c.members) } // want "ARC003: Values result 1"

// Roster only sorts names, so it is a read-only value handle.
type Roster struct{ club *Club }

func (r Roster) Names() []string { return r.club.Names() }

type League struct{ club *Club }

func (l *League) Roster() Roster { return Roster{l.club} }
