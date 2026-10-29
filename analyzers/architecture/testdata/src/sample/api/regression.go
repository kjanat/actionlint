package api

import (
	"sample/helper"
	"sample/internal/detail"
	"sync/atomic"
)

func AliasLeak() helper.Alias[detail.Record]     { return nil } // want "ARC001: public API AliasLeak exposes internal type sample/internal/detail.Record"
func PhantomLeak() helper.Phantom[detail.Record] { return 0 }   // want "ARC001: public API PhantomLeak exposes internal type sample/internal/detail.Record"
func AliasValue() helper.Alias[int]              { return nil }

type AtomicCounter struct {
	Value int64 // want "ARC004: type AtomicCounter exposes writable field Value"
}

func (c *AtomicCounter) Inc() { atomic.AddInt64(&c.Value, 1) }

type CallbackCounter struct {
	Value int64 // want "ARC004: type CallbackCounter exposes writable field Value"
}

func (c *CallbackCounter) Change(f func(*int64)) { f(&c.Value) }

type ForwardCounter struct {
	Value int64 // want "ARC004: type ForwardCounter exposes writable field Value"
}

func (c *ForwardCounter) Change(f func(*int64)) { forward(f, &c.Value) }
func forward(f func(*int64), p *int64)          { f(p) }

type DetachedCounter struct{ Value int64 }

type ImportedCounter struct {
	Value int64 // want "ARC004: type ImportedCounter exposes writable field Value"
}

func (c *ImportedCounter) Change(f func(*int64)) { helper.Forward(f, &c.Value) }

type Mutator interface{ Change(*int64) }
type InterfaceCounter struct {
	Value int64 // want "ARC004: type InterfaceCounter exposes writable field Value"
}

func (c *InterfaceCounter) Change(m Mutator) { m.Change(&c.Value) }

type HandleOwner struct{ value int64 }

func (c *HandleOwner) Handle() helper.Handle { return helper.NewHandle(&c.value) } // want "ARC003: Handle result 1"

func (c DetachedCounter) Change(f func(*int64)) { f(&c.Value) }

type ScalarCounter struct{ Value int64 }

func (c *ScalarCounter) Read(f func(int64)) { f(c.Value) }

type Pipe struct {
	Ch chan int // want "ARC002: state-owning type Pipe exposes writable field Ch"
}

func (p *Pipe) Stop() { close(p.Ch) }
