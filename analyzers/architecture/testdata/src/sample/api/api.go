package api // want package:"function flow summaries"

import (
	"bytes"
	"maps"
	"sample/helper"
	"sample/internal/detail"
	"slices"
)

// Public value records and constructors of fresh state are permitted.
type Record struct {
	Value int
	Data  []byte
}
type Owner struct {
	private []byte
	Public  int // want "ARC002: state-owning type Owner exposes writable field Public"
}

func NewOwner() *Owner               { return &Owner{private: []byte("new")} }
func (s *Owner) Borrow() []byte      { return s.private }                         // want "ARC003: Borrow result 1"
func (s *Owner) Alias() []byte       { v := s.private; return v[:len(v):len(v)] } // want "ARC003: Alias result 1"
func (s *Owner) Helper() []byte      { return helper.Borrow(s.private) }          // want "ARC003: Helper result 1"
func (s *Owner) Local() []byte       { return local(s.private) }                  // want "ARC003: Local result 1"
func local(x []byte) []byte          { return x }
func (s *Owner) Clone() []byte       { return bytes.Clone(s.private) }
func (s *Owner) CloneHelper() []byte { return helper.Detach(s.private) }
func (s *Owner) CloneSlices() []byte { return slices.Clone(s.private) }
func (s *Owner) Value() int          { return len(s.private) }
func (s *Owner) Address() *int       { return &s.Public }                // want "ARC003: Address result 1"
func (s *Owner) Wrapped() *Record    { return &Record{Data: s.private} } // want "ARC003: Wrapped result 1"
func (s *Owner) Boxed() any          { return s.private }                // want "ARC003: Boxed result 1"
func (s *Owner) Branch(ok bool) []byte { // want "ARC003: Branch result 1"
	if ok {
		return s.private
	}
	return nil
}
func (s *Owner) Tuple() (int, []byte) { return 1, s.private } // want "ARC003: Tuple result 2"

// Mutating methods imply state ownership even without a private field.
type Counter struct{ Value int } // want "ARC002: state-owning type Counter exposes writable field Value"
func (c *Counter) Increment()    { c.Value++ }

// Aliases and renamed fields follow type identity, without registration.
type Renamed struct {
	storage   map[string]int
	Arbitrary string // want "ARC002: state-owning type Renamed exposes writable field Arbitrary"
}

func (r *Renamed) Entries() map[string]int { return r.storage } // want "ARC003: Entries result 1"
func (r *Renamed) Copy() map[string]int    { return maps.Clone(r.storage) }

type Alias = Renamed

type Nested struct {
	pointers []*int
	buffers  [][]byte
}

func (n Nested) Shallow() []*int   { return slices.Clone(n.pointers) } // want "ARC003: Shallow result 1"
func (n Nested) Buffers() [][]byte { return slices.Clone(n.buffers) }  // want "ARC003: Buffers result 1"
func (n Nested) Item() *int        { return n.pointers[0] }            // want "ARC003: Item result 1"
// Opaque immutable value handles may share private storage.
type Handle struct{ data *Owner }

func (s *Owner) Handle() Handle { return Handle{data: s} }

// Implementation imports are allowed; implementation types in API shapes are not.
func InternalWork() int                          { return detail.Record{}.Value }
func Leak() detail.Record                        { return detail.Record{} } // want "ARC001: public API Leak exposes internal type sample/internal/detail.Record"
type Leaked = detail.Record                      // want "ARC001: public API Leaked exposes internal type sample/internal/detail.Record"
type LeakedSlice []detail.Record                 // want "ARC001: public API LeakedSlice exposes internal type sample/internal/detail.Record"
type Contract interface{ Read() *detail.Record } // want "ARC001: public API Contract exposes internal type sample/internal/detail.Record"
type Holder struct{ Record detail.Record }       // want "ARC001: public API Holder exposes internal type sample/internal/detail.Record"
type Opaque struct{ record detail.Record }

func (s *Owner) AppendCopy() []byte    { return append([]byte(nil), s.private...) }
func (s *Owner) Reuse() []byte         { return append(s.private, 1) }               // want "ARC003: Reuse result 1"
func (n Nested) AppendShallow() []*int { return append([]*int(nil), n.pointers...) } // want "ARC003: AppendShallow result 1"
func (s *Owner) ThroughTuple() []byte  { _, b := two(s.private); return b }          // want "ARC003: ThroughTuple result 1"
func two(x []byte) (int, []byte)       { return 0, x }
func (s *Owner) ThroughCycle() []byte  { return recurse(s.private, 1) } // want "ARC003: ThroughCycle result 1"
func recurse(x []byte, n int) []byte {
	if n == 0 {
		return x
	}
	return recurse(x, n-1)
}
func (s *Owner) Closure() func() []byte     { return func() []byte { return s.private } }   // want "ARC003: Closure result 1"
func (s *Owner) InvokedClosure() []byte     { return func() []byte { return s.private }() } // want "ARC003: InvokedClosure result 1"
func (s *Owner) MapCopy() map[string][]byte { return map[string][]byte{"data": s.private} } // want "ARC003: MapCopy result 1"
type Envelope struct{ Public []byte }
type Container struct{ hidden Envelope }

func (c *Container) NestedValue() Envelope { return c.hidden } // want "ARC003: NestedValue result 1"

type ValueRecord struct {
	Number int
	Data   []byte
}

func (v ValueRecord) WithNumber(n int) ValueRecord {
	v.Number = n
	v.Data = bytes.Clone(v.Data)
	return v
}
func (v *ValueRecord) Detached() ValueRecord {
	out := *v
	out.Number++
	out.Data = bytes.Clone(out.Data)
	return out
}

// Editing cloned outer storage must not turn a value record into an owner.
func (v ValueRecord) WithByte(b byte) ValueRecord {
	v.Data = bytes.Clone(v.Data)
	v.Data[0] = b
	return v
}

type ShallowRecord struct{ Data [][]byte } // want "ARC002: state-owning type ShallowRecord exposes writable field Data"

func (v ShallowRecord) EditNested() {
	v.Data = slices.Clone(v.Data)
	v.Data[0][0] = 1
}

type Transformer interface{ Transform([]byte) []byte }

func (s *Owner) Unknown(t Transformer) []byte { return t.Transform(s.private) } // want "ARC004: Unknown result 1"

func uncertainWrite(t Transformer, x []byte)     { b := t.Transform(x); b[0] = 1 }
func callUncertainWrite(t Transformer, x []byte) { uncertainWrite(t, x) }

type Entries struct{ values map[string]*int }

func (e *Entries) First() *int { // want "ARC003: First result 1"
	for _, value := range e.values {
		return value
	}
	return nil
}
func (s *Owner) ArrayView() *[1]byte { return (*[1]byte)(s.private) } // want "ARC003: ArrayView result 1"

type WritableHandle struct{ data *Owner }

func (h WritableHandle) Set()                   { h.data.private[0] = 1 }
func (s *Owner) WritableHandle() WritableHandle { return WritableHandle{data: s} } // want "ARC003: WritableHandle result 1"

type HandleList struct {
	writable []WritableHandle
	readonly []Handle
}

func (l *HandleList) ShallowHandles() []WritableHandle { return slices.Clone(l.writable) } // want "ARC003: ShallowHandles result 1"
func (l *HandleList) ReadonlyHandles() []Handle        { return slices.Clone(l.readonly) }
