package api

import "context"

// Lifetime returns its context's Err. The Context contract limits that error
// to nil, context.Canceled or context.DeadlineExceeded, none of which is
// storage held by the receiver.
type Lifetime struct{ ctx context.Context }

func (l *Lifetime) Err() error { return l.ctx.Err() }

// Deadline only reads Ctx, so its fields stay a plain value record.
type Deadline struct {
	Ctx   context.Context
	Value int64
}

func (d *Deadline) Expired() bool { return d.Ctx.Err() != nil }

// Other Context methods have no model; Value may return stored values.
type Values struct{ ctx context.Context }

func (v *Values) Lookup(key any) any { return v.ctx.Value(key) } // want "ARC004: Lookup result 1"
