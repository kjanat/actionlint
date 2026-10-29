package shellcheck

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type wrapper struct{ analyzer.Invocation }
type outer struct{ *wrapper }
type shadow struct {
	analyzer.Invocation
	Depth int
}
type local analyzer.Invocation
type invocationAlias = analyzer.Invocation
type invocationPointer *analyzer.Invocation

func rangeMutations(inv *analyzer.Invocation, values []int) {
	for inv.Depth = range values { // want "ALA103"
	}
	for _, inv.Depth = range values { // want "ALA103"
	}
	for inv.Depth, inv.Depth = range values { // want "ALA103" "ALA103"
	}
	for index, value := range values {
		_, _ = index, value
	}
}

func promotedFields(w *wrapper, o *outer, s *shadow) {
	w.Depth = 1             // want "ALA103"
	(w.Depth)++             // want "ALA103"
	w.Depth--               // want "ALA103"
	o.Depth += 1            // want "ALA103"
	for o.Depth = range 2 { // want "ALA103"
	}
	s.Depth = 1
	s.Invocation.Depth = 1 // want "ALA103"
	_ = w.Depth
}

func pointerMutation(p invocationPointer) {
	p.Depth++ // want "ALA103"
}

func conversions(v local, p *local, canonical analyzer.Invocation, ptr *analyzer.Invocation) {
	_ = invocationAlias(v) // want "ALA103"
	_ = invocationAlias(canonical)
	_ = analyzer.Invocation(struct { // want "ALA103"
		Depth int
		Step  diagnostic.Position
	}{Depth: 2})
	_ = analyzer.Invocation(v)               // want "ALA103"
	_ = analyzer.Invocation(local{Depth: 1}) // want "ALA103"
	_ = (*analyzer.Invocation)(p)            // want "ALA103"
	_ = analyzer.Invocation(canonical)
	_ = (*analyzer.Invocation)(ptr)
	_ = invocationPointer(ptr)
	_ = (*analyzer.Invocation)(nil)
	_ = analyzer.Invocation(local{})
	_ = analyzer.Invocation{}
	_ = new(analyzer.Invocation)
	factory := func() analyzer.Invocation { return canonical }
	_ = factory()
}
