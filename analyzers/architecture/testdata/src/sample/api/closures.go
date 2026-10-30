package api

import (
	"iter"
	"maps"
)

// A returned closure exposes the captured storage it can hand out or change.
func (s *Owner) Chunks() iter.Seq[[]byte] { // want "ARC003: Chunks result 1"
	return func(yield func([]byte) bool) { yield(s.private) }
}

type Tables struct{ m map[string]int }

func (t *Tables) Each() iter.Seq2[string, map[string]int] { // want "ARC003: Each result 1"
	return func(yield func(string, map[string]int) bool) { yield("all", t.m) }
}

type Positions struct{ items []int }

func (p *Positions) Pointers() iter.Seq[*int] { // want "ARC003: Pointers result 1"
	return func(yield func(*int) bool) {
		for i := range p.items {
			if !yield(&p.items[i]) {
				return
			}
		}
	}
}

type Tally struct{ total int }

func (t *Tally) Adder() func(int) { return func(n int) { t.total += n } } // want "ARC003: Adder result 1"

func (s *Owner) Exporter() func(*Box) { return func(b *Box) { b.Data = s.private } } // want "ARC003: Exporter result 1"

// Collecting what an exposing iterator yields keeps the aliases, though the
// iterator call itself is unresolved.
func (s *Owner) Collected() [][]byte { // want "ARC004: Collected result 1"
	var out [][]byte
	for chunk := range s.Chunks() {
		out = append(out, chunk)
	}
	return out
}

// A loop body that copies receiver storage into another captured variable
// makes that variable alias the receiver, whatever the iterator yields.
func (s *Owner) Last(steps iter.Seq[int]) []byte { // want "ARC003: Last result 1"
	var out []byte
	for range steps {
		out = s.private
	}
	return out
}

// Read-only iterators over pointer-free values or opaque handles expose nothing.
func (p *Positions) Values() iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		for i, v := range p.items {
			if !yield(i, v) {
				return
			}
		}
	}
}

func (s *Owner) Handles() iter.Seq[Handle] {
	return func(yield func(Handle) bool) { yield(Handle{data: s}) }
}

func (s *Owner) HandleThunk() func() Handle { return func() Handle { return Handle{data: s} } }

// Building a handle from two captures stores neither into the other.
type Ref struct {
	owner *Owner
	at    *int
}

func makeRef(owner *Owner, at *int) Ref { return Ref{owner: owner, at: at} }

type Index struct {
	owner *Owner
	slots map[string]*int
}

func (x *Index) Refs() iter.Seq[Ref] {
	owner, slots := x.owner, x.slots
	return func(yield func(Ref) bool) {
		for _, at := range slots {
			if !yield(makeRef(owner, at)) {
				return
			}
		}
	}
}

// Code in the package can write the private fields of a yielded handle. A
// loop body that does so writes storage reachable from the arguments the
// iterator was built from, as writing a returned handle does.
type Poker struct {
	Owner *Owner // want "ARC004: type Poker exposes writable field Owner"
}

func (p Poker) Poke() {
	for h := range p.Owner.Handles() {
		h.data.private[0] = 1
	}
}

type DirectPoker struct {
	Owner *Owner // want "ARC002: state-owning type DirectPoker exposes writable field Owner"
}

func (p DirectPoker) Poke() {
	h := p.Owner.Handle()
	h.data.private[0] = 1
}

// A loop body that keeps a yielded handle in a captured variable makes that
// variable alias the iterator's sources.
type Keeper struct {
	Owner *Owner // want "ARC004: type Keeper exposes writable field Owner"
}

func (k Keeper) Poke() {
	var kept Handle
	for h := range k.Owner.Handles() {
		kept = h
	}
	kept.data.private[0] = 1
}

// Reading what a read-only iterator yields writes nothing.
type HandleCounter struct{ Owner *Owner }

func (c HandleCounter) Count() int {
	n := 0
	for h := range c.Owner.Handles() {
		n += len(h.data.private)
	}
	return n
}

// Ranging over its own read-only iterator does not make a handle mutating.
type View struct{ items *[]int }

func (v View) All() iter.Seq[int] {
	return func(yield func(int) bool) {
		for _, x := range *v.items {
			if !yield(x) {
				return
			}
		}
	}
}

func (v View) Sum() int {
	total := 0
	for x := range v.All() {
		total += x
	}
	return total
}

func (p *Positions) View() View { return View{items: &p.items} }

// A value receiver without storage is a private copy.
type Shape interface {
	Merge(Shape) Shape
	DeepCopy() Shape
}

type Unit struct{}

func (u Unit) Merge(Shape) Shape { return u }
func (u Unit) DeepCopy() Shape   { return Unit{} }

type Point struct{ X, Y int }

func (p Point) Boxed() any { return p }

type Words struct{ list []string }

func (w Words) Boxed() any { return w } // want "ARC003: Boxed result 1"

// Joining two fresh maps yields fresh storage; writing it changes no parameter.
type Combo struct{ values map[string][]byte }

func (c Combo) With(name string, item []byte) Combo {
	values := maps.Clone(c.values)
	if values == nil {
		values = map[string][]byte{}
	}
	values[name] = item
	return Combo{values: values}
}

func (c Combo) Spread(names []string, item []byte) Combo {
	values := maps.Clone(c.values)
	for _, name := range names {
		if values == nil {
			values = map[string][]byte{}
		}
		values[name] = item
	}
	return Combo{values: values}
}

// A join that can still be the receiver's own map writes the receiver.
type SharedCombo struct{ values map[string][]byte }

func (c SharedCombo) Into(name string, item []byte) SharedCombo { // want "ARC003: Into result 1"
	values := c.values
	if values == nil {
		values = map[string][]byte{}
	}
	values[name] = item
	return SharedCombo{values: values}
}
