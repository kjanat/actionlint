package api // want package:"function flow summaries"

import (
	"defaultpolicy/internal/detail"
	"defaultpolicy/internal/state"
	"io"
	"sync/atomic"
	"time"
)

// Public contracts still cannot publish implementation-package types.
func InternalLeak() detail.Record { return detail.Record{} } // want "ARC001: public API InternalLeak exposes internal type defaultpolicy/internal/detail.Record"

type PrivateState struct {
	private int
	Public  int // want "ARC002: state-owning type PrivateState exposes writable field Public"
}

type Counter struct {
	Value int
}

func (c *Counter) Increment() { c.Value++ }

type AtomicCounter struct {
	Value int64
}

func (c *AtomicCounter) Increment() { atomic.AddInt64(&c.Value, 1) }

// A mutable DTO does not declare a private representation. Neither known
// writes nor unknown callback/stream/time effects imply exclusive ownership.
type ImportedCounter struct{ Value int }

func (c *ImportedCounter) Increment() { state.Increment(&c.Value) }

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
}

func (s *Streams) Write(data []byte) (int, error) { return s.Stdout.Write(data) }

type Warnings struct{ Warning func(string) }

func (w *Warnings) Emit(message string) { w.Warning(message) }

type Release struct{ Published time.Time }

func (r *Release) PublishedYet() bool { return !r.Published.IsZero() }

// Parser borrows the mutable graph supplied by its caller. Returning it must
// not impose a snapshot contract on an operational parser API.
type Node struct {
	Text     string
	Children []*Node
}

type Parser struct{ node *Node }

func NewParser(node *Node) *Parser { return &Parser{node: node} }
func (p *Parser) Node() *Node      { return p.node }

// A handle may intentionally mutate shared operational state. This default
// policy makes no immutability claim about its methods.
type State struct{ count int }
type Handle struct{ state *State }
type Service struct{ state State }

func (h Handle) Increment()         { h.state.count++ }
func (s *Service) Handle() Handle   { return Handle{state: &s.state} }
func (s *Service) Callback() func() { return func() { s.state.count++ } }

// An error chain retains its supplied cause's identity, including arbitrary
// concrete error types needed by errors.Is and errors.As.
type ErrorChain struct{ cause error }

func NewErrorChain(cause error) *ErrorChain { return &ErrorChain{cause: cause} }
func (e *ErrorChain) Error() string         { return e.cause.Error() }
func (e *ErrorChain) Unwrap() error         { return e.cause }

// Even a raw borrowed slice is not universally forbidden. Immutable domain
// projections require their own detachment contracts and regression tests.
type Buffer struct{ data []byte }

func (b *Buffer) Bytes() []byte { return b.data }
