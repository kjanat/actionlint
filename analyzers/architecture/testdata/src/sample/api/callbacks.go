package api

import "sample/helper"

// A method value binds its receiver. The bound receiver follows the method's
// parameters, so an imported summary is remapped before it is applied.
type Wrap struct{ s *helper.Store }

func (w *Wrap) Rower() func(int) []int         { return w.s.Row }  // want "ARC003: Rower result 1"
func (w *Wrap) Rowser() func() [][]int         { return w.s.Rows } // want "ARC003: Rowser result 1"
func (w *Wrap) Counter() func(int) int         { return w.s.Count }
func (w *Wrap) Picker() func(int, []int) []int { return w.s.Pick }

func (w *Wrap) FirstVia() []int { // want "ARC003: FirstVia result 1"
	f := w.s.Row
	return f(0)
}

func (w *Wrap) Picked(row []int) []int {
	f := w.s.Pick
	return f(0, row)
}

// A loop body that keeps what an iterator yields aliases the iterator's
// sources, whether the iterator is unknown, a local closure, a summarized
// function or an imported method value.
type Local struct{ rows [][]int }

func (l *Local) FirstLocal() []int { // want "ARC004: FirstLocal result 1"
	seq := func(yield func([]int) bool) {
		for _, r := range l.rows {
			if !yield(r) {
				return
			}
		}
	}
	for r := range seq {
		return r
	}
	return nil
}

func (w *Wrap) FirstImported() []int { // want "ARC004: FirstImported result 1"
	for r := range w.s.EachRow {
		return r
	}
	return nil
}

func eachRow(rows [][]int, yield func([]int) bool) {
	for _, r := range rows {
		if !yield(r) {
			return
		}
	}
}

func (l *Local) FirstFn() []int { // want "ARC004: FirstFn result 1"
	var out []int
	eachRow(l.rows, func(r []int) bool { out = r; return false })
	return out
}

func (l *Local) Width() int {
	n := 0
	eachRow(l.rows, func(r []int) bool { n += len(r); return true })
	return n
}

// A closure called through a function value applies its stores into
// captured variables.
func (l *Local) Indirect(c bool) []int { // want "ARC003: Indirect result 1"
	var out []int
	f := func() { out = l.rows[0] }
	if c {
		f = func() {}
	}
	f()
	return out
}

func (l *Local) Size(c bool) int {
	n := 0
	f := func() { n = len(l.rows) }
	if c {
		f = func() { n = 1 }
	}
	f()
	return n
}
