package state // want package:"function flow summaries"

import "sync/atomic"

type Operational struct {
	private int
	Public  int // want "ARC002: state-owning type Operational exposes writable field Public"
}

type Counter struct {
	Value int // want "ARC002: state-owning type Counter exposes writable field Value"
}

func (c *Counter) Increment() { c.Value++ }

type AtomicCounter struct {
	Value int64 // want "ARC004: type AtomicCounter exposes writable field Value"
}

func (c *AtomicCounter) Increment() { atomic.AddInt64(&c.Value, 1) }
